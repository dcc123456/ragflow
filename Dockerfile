# base stage
FROM ubuntu:24.04 AS base
USER root
SHELL ["/bin/bash", "-c"]

ARG NEED_MIRROR=1

WORKDIR /ragflow

# copy models downloaded via download_deps.py
RUN mkdir -p /ragflow/rag/res/deepdoc /root/.ragflow
RUN --mount=type=bind,from=infiniflow/ragflow_deps:latest,source=/huggingface.co,target=/huggingface.co \
    tar --exclude='.*' -cf - \
    /huggingface.co/InfiniFlow/text_concat_xgb_v1.0 \
    | tar -xf - --strip-components=3 -C /ragflow/rag/res/deepdoc

# Copy Tika server, NLTK data, and tiktoken from ragflow_deps image
# https://github.com/chrismattmann/tika-python
# This is the only way to run python-tika without internet access
RUN --mount=type=bind,from=infiniflow/ragflow_deps:latest,source=/,target=/deps \
    cp -r /deps/nltk_data /root/ && \
    cp /deps/tika-server-standard-3.3.0.jar /deps/tika-server-standard-3.3.0.jar.md5 /ragflow/ && \
    cp /deps/cl100k_base.tiktoken /ragflow/9b5ad71b2ce5302211f9c61530b329a4922fc6a4

ENV TIKA_SERVER_JAR="file:///ragflow/tika-server-standard-3.3.0.jar"
ENV DEBIAN_FRONTEND=noninteractive

# Setup apt with mirror support
# Python package and implicit dependencies:
# opencv-python: libglib2.0-0 libglx-mesa0 libgl1
# python-pptx:   default-jdk                              tika-server-standard-3.3.0.jar
# selenium:      libatk-bridge2.0-0                       chrome-linux64-121-0-6167-85
# Building C extensions: libpython3-dev libgtk-4-1 libnss3 xdg-utils libgbm-dev
RUN --mount=type=cache,id=ragflow_apt,target=/var/cache/apt,sharing=locked \
    apt update && \
    apt --no-install-recommends install -y ca-certificates; \
    if [ "$NEED_MIRROR" == "1" ]; then \
    sed -i 's|http://archive.ubuntu.com/ubuntu|https://mirrors.aliyun.com/ubuntu|g' /etc/apt/sources.list.d/ubuntu.sources; \
    sed -i 's|http://security.ubuntu.com/ubuntu|https://mirrors.aliyun.com/ubuntu|g' /etc/apt/sources.list.d/ubuntu.sources; \
    fi; \
    rm -f /etc/apt/apt.conf.d/docker-clean && \
    echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache && \
    chmod 1777 /tmp && \
    apt update && \
    apt install -y \
    build-essential libglib2.0-0 libglx-mesa0 libgl1 pkg-config libicu-dev libgdiplus default-jdk libatk-bridge2.0-0 libpython3-dev libgtk-4-1 libnss3 xdg-utils libgbm-dev libjemalloc-dev gnupg unzip curl wget git vim less ghostscript pandoc texlive texlive-latex-extra texlive-xetex texlive-lang-chinese fonts-freefont-ttf fonts-noto-cjk postgresql-client rsync

# Download resource from GitHub to /usr/share/infinity
RUN mkdir -p /usr/share/infinity/resource && \
    if [ "$NEED_MIRROR" == "1" ]; then \
    git clone --depth 1 --single-branch https://gitee.com/infiniflow/resource /tmp/resource; \
    else \
    git clone --depth 1 --single-branch https://github.com/infiniflow/resource.git /tmp/resource; \
    fi && \
    cp -r /tmp/resource/* /usr/share/infinity/resource && \
    rm -rf /tmp/resource

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

# Install uv from ragflow_deps image
RUN --mount=type=bind,from=infiniflow/ragflow_deps:latest,source=/,target=/deps \
    if [ "$NEED_MIRROR" == "1" ]; then \
    mkdir -p /etc/uv && \
    echo 'python-install-mirror = "https://registry.npmmirror.com/-/binary/python-build-standalone/"' > /etc/uv/uv.toml && \
    echo '[[index]]' >> /etc/uv/uv.toml && \
    echo 'url = "https://mirrors.aliyun.com/pypi/simple"' >> /etc/uv/uv.toml && \
    echo 'default = true' >> /etc/uv/uv.toml; \
    fi; \
    arch="$(uname -m)"; \
    if [ "$arch" = "x86_64" ]; then uv_arch="x86_64"; else uv_arch="aarch64"; fi; \
    tar xzf "/deps/uv-${uv_arch}-unknown-linux-gnu.tar.gz" \
    && cp "uv-${uv_arch}-unknown-linux-gnu/"* /usr/local/bin/ \
    && rm -rf "uv-${uv_arch}-unknown-linux-gnu" \
    && uv python install 3.13

ENV PYTHONDONTWRITEBYTECODE=1 DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1 \
    UV_HTTP_TIMEOUT=200 \
    UV_HTTP_RETRIES=3
ENV PATH=/root/.local/bin:$PATH

# Install profiling tools (SYS_PTRACE capability is added via K8s securityContext)
RUN uv tool install austin-dist && uv tool install austin-tui && uv tool install py-spy

# Install Node.js 22.x (Ubuntu 24.04's Node.js is too old)
RUN --mount=type=cache,id=ragflow_apt,target=/var/cache/apt,sharing=locked \
    curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
    apt purge -y nodejs npm && \
    apt autoremove -y && \
    apt update && \
    apt install -y nodejs

RUN corepack enable

# Add msssql ODBC driver
# macOS ARM64 environment, install msodbcsql18.
# general x86_64 environment, install msodbcsql17.
RUN --mount=type=cache,id=ragflow_apt,target=/var/cache/apt,sharing=locked \
    curl https://packages.microsoft.com/keys/microsoft.asc | apt-key add - && \
    curl https://packages.microsoft.com/config/ubuntu/22.04/prod.list > /etc/apt/sources.list.d/mssql-release.list && \
    apt update && \
    arch="$(uname -m)"; \
    if [ "$arch" = "arm64" ] || [ "$arch" = "aarch64" ]; then \
    # ARM64 (macOS/Apple Silicon or Linux aarch64) \
    ACCEPT_EULA=Y apt install -y unixodbc-dev msodbcsql18; \
    else \
    # x86_64 or others \
    ACCEPT_EULA=Y apt install -y unixodbc-dev msodbcsql17; \
    fi || \
    { echo "Failed to install ODBC driver"; exit 1; }

# Install Chrome and ChromeDriver for selenium (from ragflow_deps image)
RUN --mount=type=bind,from=infiniflow/ragflow_deps:latest,source=/chrome-linux64-121-0-6167-85,target=/chrome-linux64.zip \
    unzip /chrome-linux64.zip && \
    mv chrome-linux64 /opt/chrome && \
    ln -s /opt/chrome/chrome /usr/local/bin/

RUN --mount=type=bind,from=infiniflow/ragflow_deps:latest,source=/chromedriver-linux64-121-0-6167-85,target=/chromedriver-linux64.zip \
    unzip -j /chromedriver-linux64.zip chromedriver-linux64/chromedriver && \
    mv chromedriver /usr/local/bin/ && \
    rm -f /usr/bin/google-chrome

RUN --mount=type=bind,from=infiniflow/ragflow_deps:latest,source=/,target=/deps \
    if [ "$(uname -m)" = "x86_64" ]; then \
    dpkg -i /deps/libssl1.1_1.1.1f-1ubuntu2_amd64.deb; \
    elif [ "$(uname -m)" = "aarch64" ]; then \
    dpkg -i /deps/libssl1.1_1.1.1f-1ubuntu2_arm64.deb; \
    fi

# Configure pip to use mirrors if needed
RUN mkdir -p /root/.config/pip \
    && if [ "$NEED_MIRROR" == "1" ]; then \
    echo "[global]" > /root/.config/pip/pip.conf \
    && echo "index-url = https://mirrors.aliyun.com/pypi/simple/" >> /root/.config/pip/pip.conf \
    && echo "trusted-host = mirrors.aliyun.com" >> /root/.config/pip/pip.conf; \
    fi

# builder stage
FROM base AS builder
USER root

WORKDIR /ragflow

# Install Python dependencies from pyproject.toml and uv.lock
COPY pyproject.toml uv.lock ./

# https://github.com/astral-sh/uv/issues/10462
# uv records index url into uv.lock but doesn't failover among multiple indexes
RUN --mount=type=cache,id=ragflow_uv,target=/root/.cache/uv,sharing=locked \
    if [ "$NEED_MIRROR" == "1" ]; then \
    sed -i 's|pypi.org|mirrors.aliyun.com/pypi|g' uv.lock; \
    else \
    sed -i 's|mirrors.aliyun.com/pypi|pypi.org|g' uv.lock; \
    fi; \
    uv sync --python 3.13 --frozen && \
    # Ensure pip is available in the venv for runtime package installation (fixes #12651)
    .venv/bin/python3 -m ensurepip --upgrade

# Build frontend
COPY web web
COPY docs docs
RUN --mount=type=cache,id=ragflow_npm,target=/root/.npm,sharing=locked \
    --mount=type=cache,id=ragflow_vite,target=/ragflow/web/.vite-cache,sharing=locked \
    cd web && NODE_OPTIONS="--max-old-space-size=8192" npm install && \
    NODE_OPTIONS="--max-old-space-size=8192" VITE_BUILD_SOURCEMAP=false npm run build

# Get version from git (mount .git directory to compute version dynamically)
RUN --mount=type=bind,source=.git,target=/ragflow/.git \
    version_info=$(git describe --tags --match=v* --first-parent --always); \
    echo "RAGFlow version: $version_info"; \
    echo "$version_info" > /ragflow/VERSION

# production stage
FROM base AS production
USER root

WORKDIR /ragflow

# Copy Python environment and packages from builder
ENV VIRTUAL_ENV=/ragflow/.venv
COPY --from=builder ${VIRTUAL_ENV} ${VIRTUAL_ENV}
ENV PATH="${VIRTUAL_ENV}/bin:${PATH}"

ENV PYTHONPATH=/ragflow/

# Copy configuration files first (these change less frequently)
COPY docker/service_conf.yaml.template ./conf/service_conf.yaml.template
COPY docker/entrypoint.sh docker/entrypoint-parser.sh ./
RUN chmod +x ./entrypoint.sh ./entrypoint-parser.sh

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
COPY web web
COPY admin admin
COPY api api
COPY conf conf
COPY deepdoc/__init__.py deepdoc/__init__.py
COPY deepdoc/parser deepdoc/parser
COPY deepdoc/vision/__init__.py deepdoc/vision/__init__.py
COPY deepdoc/vision/recognizer.py deepdoc/vision/recognizer.py
COPY deepdoc/vision/ocr_cli.py deepdoc/vision/ocr_cli.py
COPY deepdoc/vision/dla_cli.py deepdoc/vision/dla_cli.py
COPY deepdoc/vision/tsr.py deepdoc/vision/tsr.py
COPY deepdoc/vision/tsr_cli.py deepdoc/vision/tsr_cli.py
COPY deepdoc/README.md deepdoc/README.md
COPY deepdoc/README_zh.md deepdoc/README_zh.md
COPY deepdoc/README_tr.md deepdoc/README_tr.md
COPY rag rag
COPY agent agent
COPY pyproject.toml uv.lock ./
COPY mcp mcp
COPY common common
COPY memory memory
COPY bin bin

# Copy compiled web pages
COPY --from=builder /ragflow/web/dist /ragflow/web/dist

# Copy version info
COPY --from=builder /ragflow/VERSION /ragflow/VERSION

# Set environment variables
ENV HF_ENDPOINT=https://hf-mirror.com

ENTRYPOINT ["./entrypoint.sh"]
