# builder stage
FROM infiniflow/ragflow-base:v2.1 AS builder
USER root
SHELL ["/bin/bash", "-c"]

ARG NEED_MIRROR=1

WORKDIR /ragflow

# install dependencies from uv.lock file
COPY pyproject.toml uv.lock ./

# https://github.com/astral-sh/uv/issues/10462
# uv records index url into uv.lock but doesn't failover among multiple indexes
# Also rewrite pypi.tuna.tsinghua.edu.cn to mirrors.aliyun.com/pypi so locks
# that were resolved against the Tsinghua mirror (e.g. when UV_INDEX pointed
# there) get normalized to the Aliyun mirror in NEED_MIRROR=1 builds. Without
# this, stale Tsinghua URLs slip through and `uv sync --frozen` 404s on
# packages that the Tsinghua mirror no longer carries.
RUN --mount=type=cache,id=ragflow_uv,target=/root/.cache/uv,sharing=locked \
    if [ "$NEED_MIRROR" == "1" ]; then \
        sed -i 's|pypi.org|mirrors.aliyun.com/pypi|g' uv.lock; \
        sed -i 's|pypi.tuna.tsinghua.edu.cn|mirrors.aliyun.com/pypi|g' uv.lock; \
    else \
        sed -i 's|mirrors.aliyun.com/pypi|pypi.org|g' uv.lock; \
        sed -i 's|pypi.tuna.tsinghua.edu.cn|pypi.org|g' uv.lock; \
        sed -i 's|gitee.com|github.com|g' uv.lock; \
    fi; \
    # --refresh-package litellm forces a re-download of litellm from the
    # (post-sed) URLs in uv.lock even if BuildKit's persistent uv cache mount
    # holds a stale wheel from a previous build. litellm 1.88.x has had
    # multiple internal ImportError issues (1.88.1 missing
    # DEFAULT_HEALTH_CHECK_STALENESS_MULTIPLIER, 1.88.0 wheel pulled via
    # some proxies missing RedisPipelineLpopOperation) — always re-fetching
    # the locked version avoids serving a half-broken cached copy.
    uv sync --python 3.13 --frozen --refresh-package litellm && \
    # Ensure pip is available in the venv for runtime package installation (fixes #12651)
    .venv/bin/python3 -m ensurepip --upgrade

# Install frontend dependencies — depends only on package manifests so
# web source / docs changes don't invalidate this layer.
COPY web/package.json web/package-lock.json web/.npmrc ./web/
RUN --mount=type=cache,id=ragflow_npm,target=/root/.npm,sharing=locked \
    cd web && NODE_OPTIONS="--max-old-space-size=8192" npm install

# Copy full web source and docs for the frontend build.
COPY web web
COPY docs docs
RUN --mount=type=cache,id=ragflow_npm,target=/root/.npm,sharing=locked \
    cd web && NODE_OPTIONS="--max-old-space-size=8192" VITE_BUILD_SOURCEMAP=false VITE_MINIFY=esbuild npm run build

# Get version from git (mount .git directory to compute version dynamically)
RUN --mount=type=bind,source=.git,target=/ragflow/.git \
    version_info=$(git describe --tags --match=v* --first-parent --always); \
    echo "RAGFlow version: $version_info"; \
    echo "$version_info" > /ragflow/VERSION

# production stage
FROM infiniflow/ragflow-base:v2.1 AS production
USER root

WORKDIR /ragflow

# Copy Python environment and packages from builder
ENV VIRTUAL_ENV=/ragflow/.venv
COPY --from=builder ${VIRTUAL_ENV} ${VIRTUAL_ENV}
ENV PATH="${VIRTUAL_ENV}/bin:${PATH}"

ENV PYTHONPATH=/ragflow/

# Copy configuration files first (these change less frequently)
COPY docker/service_conf.yaml.template ./conf/service_conf.yaml.template
COPY docker/entrypoint*.sh ./
RUN chmod +x ./entrypoint*.sh

# Install OpenResty (nginx with Lua support for rate limiting)
# openresty-openssl3 provides a compatible libssl.so.3 (OpenSSL 3.x) under /usr/local/openresty/openssl3/
# so that the OpenResty nginx binary does not depend on the host OS libssl version.
RUN --mount=type=cache,id=ragflow_apt,target=/var/cache/apt,sharing=locked \
    curl -fsSL https://openresty.org/package/pubkey.gpg | gpg --dearmor -o /usr/share/keyrings/openresty.gpg \
    && echo "deb [signed-by=/usr/share/keyrings/openresty.gpg] http://openresty.org/package/ubuntu noble main" > /etc/apt/sources.list.d/openresty.list \
    && apt -o Acquire::Retries=5 update \
    && apt -o Acquire::Retries=5 install -y openresty openresty-openssl3 \
    && ln -sf /usr/local/openresty/nginx/sbin/nginx /usr/sbin/nginx \
    && echo "/usr/local/openresty/openssl3/lib" > /etc/ld.so.conf.d/openresty-openssl3.conf \
    && ldconfig


# Copy nginx configuration for frontend serving (with Lua rate limiter)
# OpenResty installs to /usr/local/openresty/nginx/; create /etc/nginx/ symlink tree
RUN mkdir -p /etc/nginx/conf.d /var/log/nginx && \
    ln -sf /usr/local/openresty/nginx/conf/mime.types /etc/nginx/mime.types


COPY docker/nginx/ragflow.conf.golang docker/nginx/ragflow.conf.python docker/nginx/ragflow.conf.hybrid docker/nginx/nginx.conf docker/nginx/proxy.conf docker/nginx/rate_limit.lua /etc/nginx/
RUN mv /etc/nginx/ragflow.conf.golang /etc/nginx/conf.d/ragflow.conf.golang && \
    mv /etc/nginx/ragflow.conf.python /etc/nginx/conf.d/ragflow.conf.python && \
    mv /etc/nginx/ragflow.conf.hybrid /etc/nginx/conf.d/ragflow.conf.hybrid && \
    rm -f /etc/nginx/sites-enabled/default

# Copy application code (these change more frequently)
COPY admin admin
COPY api api
COPY conf conf
COPY deepdoc deepdoc
COPY rag rag
COPY agent agent
COPY pyproject.toml uv.lock ./
COPY mcp mcp
COPY common common
COPY memory memory
COPY bin bin
COPY tools/scripts tools/scripts

# Copy compiled web pages
COPY --from=builder /ragflow/web/dist /ragflow/web/dist

# Copy version info
COPY --from=builder /ragflow/VERSION /ragflow/VERSION

# Set environment variables
ENV HF_ENDPOINT=https://hf-mirror.com

ENTRYPOINT ["./entrypoint.sh"]
