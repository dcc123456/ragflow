--[[
  Billing Rate Limiter — Nginx (OpenResty) Lua Module

  Three-tier rate limiting (T1 → T2 → T3):

    T1 — Per-tenant (user-id) rate limiting via Redis token bucket
      1. Extract Bearer token from Authorization header
      2. Look up tenant_id from Redis (billing:token:<token>)
         — with ngx.shared.dict LRU cache to reduce Redis lookups
      3. Look up rate limits from Redis (billing:ratelimit:<tenant_id>)
         — with ngx.shared.dict LRU cache
      4. Enforce per-minute token bucket via Redis Lua EVALSHA

    T2 — Per-IP rate limiting via Redis (cross-pod accurate)
      Applied when tenant cannot be identified (no token, session token,
      unknown token), or when T1 encounters an error.

    T3 — Per-IP rate limiting via ngx.shared.dict (per-pod, last resort)
      Applied only when Redis is completely unavailable.

  Redis keys used:
    billing:token:<token>                -> tenant_id  (string, set by Python sync)
    billing:ratelimit:<tenant_id>        -> JSON {rpm}  (set by Python sync)
    billing:tb:min:<tenant_id>           -> {tokens, timestamp}  (per-minute bucket)
    billing:fb:sec:<ip>                  -> counter  (per-second IP fallback via Redis)
    billing:fb:min:<ip>                  -> counter  (per-minute IP fallback via Redis)
]]

local _M = {}
_M._VERSION = "2.0.0"

---------------------------------------------------------------------------
-- Constants
---------------------------------------------------------------------------
local CACHE_TTL_OK    = 60       -- cache positive lookups for 60s
local CACHE_TTL_MISS  = 5        -- cache misses for only 5s (quick retry)
local POOL_IDLE       = 10000    -- keepalive idle timeout ms
local POOL_SIZE       = 64       -- max connections in pool

-- Fallback per-IP rate limits (applied when tenant is unknown)
local FALLBACK_RPM    = 120      -- 120 requests per minute per IP
local FALLBACK_RPS    = 30       -- 30 requests per second per IP

-- Token bucket Lua script executed inside Redis (same algorithm as Python/Go)
local TOKEN_BUCKET_SCRIPT = [[
local key       = KEYS[1]
local capacity  = tonumber(ARGV[1])
local rate      = tonumber(ARGV[2])
local now       = tonumber(ARGV[3])
local cost      = tonumber(ARGV[4])

local data = redis.call("HMGET", key, "tokens", "timestamp")
local tokens = tonumber(data[1])
local last_ts = tonumber(data[2])

if tokens == nil then
    tokens = capacity
    last_ts = now
end

local delta = math.max(0, now - last_ts)
tokens = math.min(capacity, tokens + delta * rate)

if tokens < cost then
    return {0, tostring(math.floor(tokens))}
end

tokens = tokens - cost

redis.call("HMSET", key,
    "tokens", tokens,
    "timestamp", now
)

redis.call("EXPIRE", key, math.ceil(capacity / rate * 2))

return {1, tostring(math.floor(tokens))}
]]

-- Will be populated by init_worker via SCRIPT LOAD
local _script_sha = nil

-- Fixed-window counter Lua script for per-IP rate limiting (Redis-based).
-- Atomically increments a counter, sets TTL on first write, returns count + TTL.
-- KEYS[1]   = counter key (e.g. billing:fb:sec:<ip>)
-- ARGV[1]   = window TTL in seconds
local IP_RATE_SCRIPT = [[
local key = KEYS[1]
local ttl = tonumber(ARGV[1])
local count = redis.call("INCR", key)
if count == 1 then
    redis.call("EXPIRE", key, ttl)
end
local remain = redis.call("TTL", key)
return {count, remain}
]]

-- Separate SHA cache for the IP rate script (avoids collision with token bucket SHA)
local _ip_script_sha = nil

---------------------------------------------------------------------------
-- Shared-dict cache helpers
---------------------------------------------------------------------------
local function cache_get(key)
    local cache = ngx.shared.billing_cache
    if not cache then return nil end
    return cache:get(key)
end

local function cache_set(key, value, ttl)
    local cache = ngx.shared.billing_cache
    if not cache then return end
    cache:set(key, value, ttl or CACHE_TTL_OK)
end

---------------------------------------------------------------------------
-- Redis connection helpers
---------------------------------------------------------------------------
local function _get_redis_config()
    local host = os.getenv("RATELIMIT_REDIS_HOST")
    if not host or host == "" then host = "redis" end
    local port = os.getenv("RATELIMIT_REDIS_PORT")
    if not port or port == "" then port = 6379 else port = tonumber(port) end
    local password = os.getenv("RATELIMIT_REDIS_PASSWORD")
    if not password then password = "" end
    local db = os.getenv("RATELIMIT_REDIS_DB")
    if not db or db == "" then db = 1 else db = tonumber(db) end
    return {
        host     = host,
        port     = port,
        password = password,
        db       = db,
    }
end

--- Get a Redis connection from pool (or create a new one).
--- Returns red, err.  Caller must call _release_redis(red) when done.
local function _get_redis()
    local redis = require "resty.redis"
    local red = redis:new()
    red:set_timeouts(200, 200, 200)  -- 200ms connect/read/write

    local cfg = _get_redis_config()
    local ok, err = red:connect(cfg.host, cfg.port)
    if not ok then
        return nil, "connect: " .. (err or "unknown")
    end

    if cfg.password and cfg.password ~= "" then
        local ok2, err2 = red:auth(cfg.password)
        if not ok2 then
            return nil, "auth: " .. (err2 or "unknown")
        end
    end

    if cfg.db and cfg.db ~= 0 then
        local ok3, err3 = red:select(cfg.db)
        if not ok3 then
            return nil, "select db: " .. (err3 or "unknown")
        end
    end

    return red, nil
end

--- Return a Redis connection to the pool.
local function _release_redis(red)
    if not red then return end
    local ok, err = red:set_keepalive(POOL_IDLE, POOL_SIZE)
    if not ok then
        ngx.log(ngx.WARN, "rate_limit: keepalive failed: ", err)
    end
end

--- Load the script SHA once (lazy, per-worker).
local function _ensure_script_sha(red)
    if _script_sha then return true end
    local sha, err = red:script("load", TOKEN_BUCKET_SCRIPT)
    if not sha then
        ngx.log(ngx.WARN, "rate_limit: script load failed: ", err)
        return false
    end
    _script_sha = sha
    return true
end

--- Run the token-bucket script with EVALSHA (falls back to EVAL).
local function _run_token_bucket(red, key, capacity, rate, now_sec, cost)
    if not _ensure_script_sha(red) then
        -- Fallback to plain EVAL
        return red:eval(TOKEN_BUCKET_SCRIPT, 1, key,
            tostring(capacity), tostring(rate), tostring(now_sec), tostring(cost))
    end

    local res, err = red:evalsha(_script_sha, 1, key,
        tostring(capacity), tostring(rate), tostring(now_sec), tostring(cost))
    if err and err:find("NOSCRIPT") then
        -- Script flushed from Redis, reload
        _script_sha = nil
        if not _ensure_script_sha(red) then
            -- Reload also failed, fallback to plain EVAL
            return red:eval(TOKEN_BUCKET_SCRIPT, 1, key,
                tostring(capacity), tostring(rate), tostring(now_sec), tostring(cost))
        end
        res, err = red:evalsha(_script_sha, 1, key,
            tostring(capacity), tostring(rate), tostring(now_sec), tostring(cost))
    end
    return res, err
end

--- Run the fixed-window IP rate script via Redis EVALSHA (falls back to EVAL).
--- Returns {count, ttl} on success, or nil, err on failure.
local function _run_ip_rate_script(red, key, window_ttl)
    -- Lazy-load script SHA (separate from token-bucket SHA)
    if not _ip_script_sha then
        local sha, err = red:script("load", IP_RATE_SCRIPT)
        if not sha then
            ngx.log(ngx.WARN, "rate_limit: ip script load failed: ", err)
            return red:eval(IP_RATE_SCRIPT, 1, key, tostring(window_ttl))
        end
        _ip_script_sha = sha
    end

    local res, err = red:evalsha(_ip_script_sha, 1, key, tostring(window_ttl))
    if err and err:find("NOSCRIPT") then
        _ip_script_sha = nil
        local sha2, err2 = red:script("load", IP_RATE_SCRIPT)
        if not sha2 then
            return red:eval(IP_RATE_SCRIPT, 1, key, tostring(window_ttl))
        end
        _ip_script_sha = sha2
        res, err = red:evalsha(_ip_script_sha, 1, key, tostring(window_ttl))
    end
    return res, err
end

---------------------------------------------------------------------------
-- Helpers
---------------------------------------------------------------------------

--- Extract Bearer token from Authorization header
-- @return token string or nil
local function extract_bearer_token()
    local auth = ngx.req.get_headers()["Authorization"]
    if not auth then return nil end

    -- "Bearer <token>" or just "<token>"
    local parts = {}
    for w in auth:gmatch("%S+") do
        parts[#parts + 1] = w
    end
    if #parts >= 2 then
        return parts[2]
    elseif #parts == 1 and #parts[1] >= 32 then
        return parts[1]
    end
    return nil
end

--- Extract client IP.
-- Prefers $remote_addr which reflects the actual TCP peer after nginx's
-- ngx_http_realip_module rewrites it from X-Forwarded-For / X-Real-IP
-- (when configured with set_real_ip_from / real_ip_header).
-- Falls back to X-Real-IP then $remote_addr without realip module.
-- NOTE: To use X-Forwarded-For, configure the nginx realip module in your
-- server block so that $remote_addr is set correctly from a trusted proxy:
--   set_real_ip_from 10.0.0.0/8;
--   real_ip_header  X-Forwarded-For;
-- @return ip string
local function get_client_ip()
    -- $remote_addr is always set and, when the realip module is active,
    -- already contains the rewritten client IP from the trusted header.
    local remote = ngx.var.remote_addr
    if remote and remote ~= "" then
        return remote
    end
    return "0.0.0.0"
end

--- Check if a path should be exempt from rate limiting
local function is_exempt_path()
    local uri = ngx.var.uri
    -- Health checks, billing webhooks, static files, login/logout
    if uri == "/" or uri == "/live" or uri == "/healthz" then return true end
    if uri:match("^/v1/user/(login|logout)") then return true end
    if uri:match("^/v1/system/config") then return true end
    if uri:match("^/v1/billing/webhook") then return true end
    if uri:match("^/v1/billing/success") then return true end
    if uri:match("^/v1/billing/cancel") then return true end
    if uri:match("^/static/") then return true end
    if uri:match("^/metrics") then return true end
    if uri:match("^/apidocs") then return true end
    if uri:match("^/apispec") then return true end
    if uri:match("^/flasgger") then return true end
    return false
end

---------------------------------------------------------------------------
-- T3 — Per-IP rate limiting via ngx.shared.dict (last resort)
-- Only used when Redis is completely unavailable. Per-pod, not cross-pod accurate.
---------------------------------------------------------------------------

--- Fixed-window counter via ngx.shared.dict.
--- Returns true if within limits, false if rate-limited.
local function _local_rate_check(key_prefix, ip, limit, window_sec)
    local cache = ngx.shared.billing_cache
    if not cache then return true end  -- no cache → pass through

    local now_sec = ngx.now()
    local window_key = key_prefix .. ip .. ":" .. tostring(math.floor(now_sec / window_sec))
    local count, err = cache:incr(window_key, 1, 0, window_sec * 2)
    if not count then
        ngx.log(ngx.WARN, "rate_limit: shared_dict incr failed: ", err)
        return true  -- fail-open
    end
    return count <= limit
end

--- T3: apply per-IP rate limit using only ngx.shared.dict.
--- Returns true if the request was rejected (429 sent), false otherwise.
local function _t3_local_ip_rate_limit()
    local ip = get_client_ip()
    if not ip or ip == "" then return false end

    -- Per-second burst
    if not _local_rate_check("fb:sec:", ip, FALLBACK_RPS, 1) then
        ngx.header["X-RateLimit-Limit"] = tostring(FALLBACK_RPM)
        ngx.header["X-RateLimit-Remaining"] = "0"
        ngx.header["Retry-After"] = "1"
        ngx.status = 429
        ngx.header["Content-Type"] = "application/json"
        ngx.say('{"code":429,"message":"Rate limit exceeded. Please slow down your requests.","data":null}')
        return ngx.exit(429)
    end

    -- Per-minute sustained
    if not _local_rate_check("fb:min:", ip, FALLBACK_RPM, 60) then
        ngx.header["X-RateLimit-Limit"] = tostring(FALLBACK_RPM)
        ngx.header["X-RateLimit-Remaining"] = "0"
        ngx.header["Retry-After"] = "2"
        ngx.status = 429
        ngx.header["Content-Type"] = "application/json"
        ngx.say('{"code":429,"message":"Rate limit exceeded. Too many requests per minute.","data":null}')
        return ngx.exit(429)
    end

    return false
end

---------------------------------------------------------------------------
-- T2 — Per-IP rate limiting via Redis (cross-pod accurate)
-- Falls back to T3 (ngx.shared.dict) if Redis is down.
---------------------------------------------------------------------------

--- T2: per-IP rate limiting via Redis fixed-window counters.
--- Falls back to T3 if Redis is unavailable.
--- If `red` is provided, reuses that connection (caller must release it).
--- If `red` is nil, opens and releases its own connection.
--- Returns true if the request was rejected (429 sent), false otherwise.
local function _t2_redis_ip_rate_limit(red)
    local ip = get_client_ip()
    if not ip or ip == "" then return false end

    local own_conn = false
    if not red then
        local err
        red, err = _get_redis()
        if not red then
            ngx.log(ngx.WARN, "rate_limit: redis unavailable for IP fallback, using shared.dict: ", err)
            return _t3_local_ip_rate_limit()
        end
        own_conn = true
    end

    -- Per-second burst via Redis
    local sec_key = "billing:fb:sec:" .. ip
    local res_sec, err_sec = _run_ip_rate_script(red, sec_key, 2)  -- TTL = 2s
    if err_sec then
        ngx.log(ngx.WARN, "rate_limit: redis IP per-sec failed: ", err_sec)
        if own_conn then _release_redis(red) end
        return _t3_local_ip_rate_limit()
    end

    if type(res_sec) == "table" and tonumber(res_sec[1]) > FALLBACK_RPS then
        if own_conn then _release_redis(red) end
        ngx.header["X-RateLimit-Limit"] = tostring(FALLBACK_RPM)
        ngx.header["X-RateLimit-Remaining"] = "0"
        ngx.header["Retry-After"] = "1"
        ngx.status = 429
        ngx.header["Content-Type"] = "application/json"
        ngx.say('{"code":429,"message":"Rate limit exceeded. Please slow down your requests.","data":null}')
        return ngx.exit(429)
    end

    -- Per-minute sustained via Redis
    local min_key = "billing:fb:min:" .. ip
    local res_min, err_min = _run_ip_rate_script(red, min_key, 120)  -- TTL = 120s
    if err_min then
        ngx.log(ngx.WARN, "rate_limit: redis IP per-min failed: ", err_min)
        if own_conn then _release_redis(red) end
        return _t3_local_ip_rate_limit()
    end

    if type(res_min) == "table" and tonumber(res_min[1]) > FALLBACK_RPM then
        if own_conn then _release_redis(red) end
        ngx.header["X-RateLimit-Limit"] = tostring(FALLBACK_RPM)
        ngx.header["X-RateLimit-Remaining"] = "0"
        ngx.header["Retry-After"] = "2"
        ngx.status = 429
        ngx.header["Content-Type"] = "application/json"
        ngx.say('{"code":429,"message":"Rate limit exceeded. Too many requests per minute.","data":null}')
        return ngx.exit(429)
    end

    if own_conn then _release_redis(red) end
    return false
end

---------------------------------------------------------------------------
-- T1 — Per-tenant (user-id) rate limiting via Redis token bucket
-- Falls back to T2 (Redis IP) on any error, which in turn falls back to T3.
---------------------------------------------------------------------------

--- Resolve tenant_id from token via cache → Redis → miss cache.
--- Returns tenant_id, red (open connection), or nil, nil on failure.
--- Caller is responsible for releasing `red` when tenant_id is returned.
local function _resolve_tenant(token)
    local cache_key_token = "t:" .. token
    local cached_tenant = cache_get(cache_key_token)

    -- Cached miss — no tenant for this token
    if cached_tenant == "__miss__" then
        return nil, nil
    end

    -- Cached hit — tenant_id found in shared dict
    if cached_tenant then
        return cached_tenant, nil  -- no Redis connection needed for cache hit
    end

    -- No cache entry — look up from Redis
    local red, err = _get_redis()
    if not red then
        ngx.log(ngx.WARN, "rate_limit: redis unavailable for tenant lookup: ", err)
        return nil, nil
    end

    local token_key = "billing:token:" .. token
    local tid, terr = red:get(token_key)
    if terr then
        ngx.log(ngx.WARN, "rate_limit: redis get tenant failed: ", terr)
        _release_redis(red)
        return nil, nil
    end

    if not tid or tid == ngx.null then
        -- Token not in Redis — session token or unknown.
        -- Cache the miss briefly to avoid hammering Redis for the same token.
        cache_set(cache_key_token, "__miss__", CACHE_TTL_MISS)
        _release_redis(red)
        return nil, nil
    end

    -- Cache the tenant lookup
    cache_set(cache_key_token, tid, CACHE_TTL_OK)
    return tid, red
end

--- Look up rate limits for a tenant from cache → Redis.
--- Returns limits table, red (open connection or nil if cached).
--- Caller is responsible for releasing `red` when non-nil.
local function _resolve_limits(tenant_id, red)
    local cache_key_limits = "l:" .. tenant_id
    local cached_limits_json = cache_get(cache_key_limits)

    if cached_limits_json then
        local cjson = require "cjson.safe"
        local limits = cjson.decode(cached_limits_json)
        if limits then
            return limits, nil  -- cache hit, no Redis needed
        end
    end

    -- Need to fetch from Redis
    if not red then
        local err
        red, err = _get_redis()
        if not red then
            ngx.log(ngx.WARN, "rate_limit: redis unavailable for limits lookup: ", err)
            return nil, nil
        end
    end

    local ratelimit_key = "billing:ratelimit:" .. tenant_id
    local lj, lerr = red:get(ratelimit_key)
    if lerr then
        ngx.log(ngx.WARN, "rate_limit: redis get limits failed: ", lerr)
        _release_redis(red)
        return nil, nil
    end

    if not lj or lj == ngx.null then
        ngx.log(ngx.WARN, "rate_limit: no limits configured for tenant ", tenant_id)
        _release_redis(red)
        return nil, nil
    end

    cache_set(cache_key_limits, lj, CACHE_TTL_OK)

    local cjson = require "cjson.safe"
    local limits = cjson.decode(lj)
    if not limits then
        ngx.log(ngx.WARN, "rate_limit: failed to decode limits JSON: ", lj)
        _release_redis(red)
        return nil, nil
    end

    return limits, red
end

--- T1: enforce per-tenant rate limits using Redis token bucket.
--- Returns:
---   "ok"    — tenant resolved and request allowed (no need for T2)
---   true    — request rejected (429 already sent)
---   false   — tenant could not be resolved or error occurred (fall through to T2)
local function _t1_tenant_rate_limit(token)
    -- Step 1: Resolve tenant_id from token
    local tenant_id, red = _resolve_tenant(token)
    if not tenant_id then
        -- Cannot identify tenant — fall through to T2
        return false
    end

    -- Step 2: Resolve rate limits for this tenant
    local limits, limits_red = _resolve_limits(tenant_id, red)
    if not limits then
        -- No limits configured or error — clean up and fall through to T2
        if red then _release_redis(red) end
        return false
    end

    -- Use the Redis connection from limits lookup (may be the same as tenant lookup)
    red = limits_red or red

    if not red then
        -- All info was cached — still need Redis for token bucket
        local err
        red, err = _get_redis()
        if not red then
            ngx.log(ngx.WARN, "rate_limit: redis unavailable for token bucket: ", err)
            return false
        end
    end

    -- Unpack limits:
    -- rpm = requests per minute (capacity for per-minute bucket)
    local rpm       = tonumber(limits.rpm)       or 500

    -- Guard: treat 0 rpm as "block all"
    if rpm <= 0 then
        _release_redis(red)
        ngx.header["Retry-After"] = "60"
        ngx.status = 429
        ngx.header["Content-Type"] = "application/json"
        ngx.say('{"code":429,"message":"API access is currently disabled for this account.","data":null}')
        return ngx.exit(429)
    end

    -- Step 3: Enforce per-minute rate limit using token bucket
    local now_sec = ngx.now()
    local min_key = "billing:tb:min:" .. tenant_id
    -- rate = rpm/60 tokens per second, capacity = rpm, cost = 1
    local min_rate = rpm / 60.0

    local res_min, err_min = _run_token_bucket(red, min_key, rpm, min_rate, now_sec, 1)
    if err_min then
        ngx.log(ngx.WARN, "rate_limit: per-minute eval failed: ", err_min)
        _release_redis(red)
        return false  -- fall through to T2
    end

    if type(res_min) == "table" and tonumber(res_min[1]) == 0 then
        -- Rate limited — calculate wait time
        local wait_sec = (min_rate > 0) and math.ceil(1 / min_rate) or 60
        _release_redis(red)
        ngx.header["X-RateLimit-Limit"] = tostring(rpm)
        ngx.header["X-RateLimit-Remaining"] = "0"
        ngx.header["Retry-After"] = tostring(math.max(1, wait_sec))
        ngx.status = 429
        ngx.header["Content-Type"] = "application/json"
        ngx.say('{"code":429,"message":"Rate limit exceeded. Too many requests per minute.","data":null}')
        return ngx.exit(429)
    end

    -- Step 4: Add rate limit info headers for successful requests
    ngx.header["X-RateLimit-Limit"] = tostring(rpm)
    if type(res_min) == "table" and res_min[2] then
        ngx.header["X-RateLimit-Remaining"] = res_min[2]
    end

    -- Set tenant_id header for downstream use
    ngx.req.set_header("X-Billing-Tenant-Id", tenant_id)

    _release_redis(red)
    return "ok"  -- tenant resolved and request allowed
end

---------------------------------------------------------------------------
-- Core entry point
---------------------------------------------------------------------------

--- Run rate limiting logic (T1 → T2 → T3 cascade)
function _M.check()
    -- Skip for non-API paths
    if is_exempt_path() then return end

    -- Allow disabling rate limiting entirely (e.g. for CI/test environments)
    local disabled = os.getenv("RATELIMIT_DISABLED")
    if disabled and (disabled == "1" or disabled:lower() == "true") then
        return
    end

    local token = extract_bearer_token()

    -- T1: Per-tenant rate limiting (only if token is present)
    if token then
        local result = _t1_tenant_rate_limit(token)
        if result == true then return end   -- 429 already sent
        if result == "ok" then return end   -- tenant resolved, request allowed
        -- result == false: tenant not resolved or error — fall through to T2
    end

    -- T2: Per-IP rate limiting via Redis
    local rejected = _t2_redis_ip_rate_limit(nil)
    if rejected then return end  -- 429 already sent
end

return _M
