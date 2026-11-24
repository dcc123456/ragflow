# Codex app-server, packaged for the mode-8 engine.
#
# The app-server binary is NOT compiled during the image build. Produce it first with
# ./build-local.sh, which compiles the pinned Codex source tree. The image only copies it in,
# so the build is reproducible and does not need a Rust toolchain or the Codex sources.
#
#   ./build-local.sh && docker build -t infiniflow/codex:<version> .
#
# The image carries no model credentials: the mode-8 engine declares the model provider per
# thread at run time.
#
# Base is glibc (the locally built binary links glibc; the official release is a static musl
# executable, which also runs here). It is a full userland on purpose: the CONTAINER is this
# deployment's isolation boundary, so a mode-8 turn may legitimately run ordinary local
# commands. python3 backs mode 8's "generate and execute a script" capability, and the CA
# bundle lets the app-server reach the model provider over TLS.
FROM ubuntu:26.04

# Ubuntu's own archive is slow to reach from mainland China. Point apt at a domestic mirror;
# pass --build-arg UBUNTU_MIRROR= (empty) to keep the image's own sources. The sed covers both
# the legacy one-line list and the deb822 *.sources format newer releases ship.
ARG UBUNTU_MIRROR=mirrors.aliyun.com
RUN if [ -n "$UBUNTU_MIRROR" ]; then \
        for f in /etc/apt/sources.list /etc/apt/sources.list.d/*.sources; do \
            [ -f "$f" ] && sed -i \
                -e "s|//archive.ubuntu.com|//$UBUNTU_MIRROR|g" \
                -e "s|//security.ubuntu.com|//$UBUNTU_MIRROR|g" \
                "$f"; \
        done || true; \
    fi \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        git \
        libbz2-1.0 \
        libgcc-s1 \
        libjitterentropy3 \
        libssl3t64 \
        python3 \
        zlib1g \
    && rm -rf /var/lib/apt/lists/*

# Run unprivileged, with a writable CODEX_HOME (config/state) and an isolated work dir.
RUN useradd --uid 10001 --create-home --home-dir /home/codex --shell /usr/sbin/nologin codex \
    && mkdir -p /home/codex/.codex /work \
    && chown -R codex:codex /home/codex /work
ENV CODEX_HOME=/home/codex/.codex

COPY entrypoint.sh /usr/local/bin/codex-entrypoint.sh

# Placed here by ./build-local.sh (git-ignored; it sets the executable bit). COPY preserves
# the mode, so there is deliberately NO `RUN chmod` — chmod would re-store the whole binary as
# a second layer and double the image size. Assert the mode instead, so a build after a
# forgotten step fails clearly.
COPY codex-app-server /usr/local/bin/codex-app-server
RUN test -x /usr/local/bin/codex-app-server && test -x /usr/local/bin/codex-entrypoint.sh \
    || { echo "error: codex-app-server/entrypoint.sh not executable — run build-local.sh before building" >&2; exit 1; }

USER codex
WORKDIR /work
EXPOSE 8790
ENTRYPOINT ["/usr/local/bin/codex-entrypoint.sh"]
