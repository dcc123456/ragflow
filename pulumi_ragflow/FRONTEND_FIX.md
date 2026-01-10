# Frontend 404 Fix - RAGFlow on Kubernetes

## Problem

When accessing `http://ragflow.local` through the Gateway API, the browser received a 404 error instead of the RAGFlow frontend homepage. HTTP API tests passed (`/v1/*` and `/api/*` endpoints worked), but the root path `/` returned 404.

## Root Cause

1. **Missing nginx configuration**: The nginx server running in the RAGFlow pod was using the default Ubuntu nginx configuration, which only serves a "Welcome to nginx" page on port 80.

2. **No HTTPRoute rule for frontend**: The HTTPRoute only had rules for `/v1` and `/api` paths, routing them to backend API ports (9380, 9381), but had no rule for the root path `/` to serve the frontend.

3. **Frontend files present but not served**: The frontend files exist at `/ragflow/web/dist/` inside the pod, but nginx wasn't configured to serve them.

## Solution

The fix involved three steps:

### 1. Configure nginx to Serve Frontend Files

Created proper nginx configuration at `/etc/nginx/conf.d/ragflow.conf`:

```nginx
server {
    listen 80;
    server_name _;
    root /ragflow/web/dist;
    index index.html;

    gzip on;
    gzip_min_length 1k;
    gzip_comp_level 9;
    gzip_types text/plain application/javascript application/x-javascript text/css application/xml text/javascript application/x-httpd-php image/jpeg image/gif image/png;
    gzip_vary on;
    gzip_disable "MSIE [1-6]\.";

    # Admin API - port 9381
    location ~ ^/api/v1/admin {
        proxy_pass http://localhost:9381;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Backend APIs - port 9380
    location ~ ^/(v1|api) {
        proxy_pass http://localhost:9380;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Frontend files
    location / {
        try_files $uri $uri/ /index.html;
    }

    # Static assets caching
    location ~ ^/static/(css|js|media)/ {
        expires 10y;
        access_log off;
    }
}
```

Also disabled the default nginx site:
```bash
rm -f /etc/nginx/sites-enabled/default
nginx -s reload
```

### 2. Expose Port 80 in Service

Added port 80 to the `ragflow` Service:

```yaml
spec:
  ports:
  - name: api
    port: 9380
    targetPort: 9380
  - name: admin
    port: 9381
    targetPort: 9381
  - name: mcp
    port: 9382
    targetPort: 9382
  - name: http  # NEW
    port: 80    # NEW
    targetPort: 80  # NEW
```

### 3. Update HTTPRoute

Added a new rule to route the root path `/` to port 80:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: ragflow-http-route
  namespace: ragflow
spec:
  hostnames:
  - ragflow.local
  parentRefs:
  - group: gateway.networking.k8s.io
    kind: Gateway
    name: ragflow-gateway
    namespace: ragflow
    sectionName: http
  rules:
  # Backend APIs - port 9380
  - backendRefs:
    - kind: Service
      name: ragflow
      port: 9380
    matches:
    - path:
        type: PathPrefix
        value: /v1
    - path:
        type: PathPrefix
        value: /api

  # Admin API - port 9381
  - backendRefs:
    - kind: Service
      name: ragflow
      port: 9381
    matches:
    - path:
        type: PathPrefix
        value: /api/v1/admin

  # Frontend - port 80 (NEW)
  - backendRefs:
    - kind: Service
      name: ragflow
      port: 80
    matches:
    - path:
        type: PathPrefix
        value: /
```

## Verification

After applying the fix:

1. **Frontend works**: `curl http://ragflow.local/` returns the RAGFlow HTML page
2. **API endpoints work**: `curl http://ragflow.local/v1/user/login` works correctly
3. **Browser can access**: Opening `http://ragflow.local` in a browser shows the RAGFlow UI

## Permanent Implementation

To make this fix permanent across deployments, update the Pulumi code in `main.go`:

1. Create a ConfigMap with the nginx configuration
2. Mount the ConfigMap as a volume in the RAGFlow Deployment
3. Update the Service to include port 80
4. Update the HTTPRoute to include the root path rule

See the next section for Pulumi implementation details.

## Files Modified

- `/etc/nginx/conf.d/ragflow.conf` - Created nginx configuration in the pod
- `/etc/nginx/sites-enabled/default` - Removed default nginx configuration
- Service `ragflow` - Added port 80
- HTTPRoute `ragflow-http-route` - Added root path `/` rule

## Testing Commands

```bash
# Test frontend
curl -s http://ragflow.local/ | head -5

# Test API
curl -s http://ragflow.local/v1/user/login -X OPTIONS -I

# Test with HOST_ADDRESS
export HOST_ADDRESS=http://ragflow.local
pytest test/testcases/test_http_api/
```

## Related Issues

- Parser pod process restart issue (fixed by removing `sys.exit()` in task_executor.py:1195)
- nginx configuration not deployed in container (fixed by manual configuration)
- HTTPRoute missing frontend path rule (fixed by updating HTTPRoute)
