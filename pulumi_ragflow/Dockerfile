# Dockerfile for Pulumi RAGFlow deployment (multi-stage)
# Stage 1: Compile Go binary
# Stage 2: Minimal runtime with only Pulumi CLI + binary + plugins
#
# Build context must be the repo ROOT (not pulumi_ragflow/):
#   docker build -f pulumi_ragflow/Dockerfile .

# ============== Stage 1: Build ==============
FROM pulumi/pulumi-go:latest AS builder

WORKDIR /app

# Copy Go module files first for better layer caching
COPY pulumi_ragflow/go.mod pulumi_ragflow/go.sum ./

ENV GOPROXY=https://goproxy.cn,https://goproxy.io,direct

RUN go mod download

# Copy Go source files
COPY pulumi_ragflow/*.go ./

# Static link for portability to slim runtime image
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o pulumi-ragflow .



# ============== Stage 2: Runtime ==============
FROM pulumi/pulumi-go:latest

# Pre-download Pulumi plugins (versions must match go.mod)
RUN pulumi plugin install resource kubernetes v4.24.1 && \
    pulumi plugin install resource alicloud v3.94.0 && \
    pulumi plugin install resource random v4.19.1

WORKDIR /app

# Copy pre-compiled binary from builder
COPY --from=builder /app/pulumi-ragflow ./

# Copy Pulumi config files
COPY pulumi_ragflow/Pulumi.yaml pulumi_ragflow/Pulumi.ali_k8s.yaml ./

# Copy external files referenced by Go code via ../docker/ paths
# Go code uses readFileContent("../docker/..."), working dir is /app,
# so ../docker/ resolves to /docker/
COPY docker/rabbitmq-conf/definitions.json /docker/rabbitmq-conf/definitions.json
COPY docker/rabbitmq-conf/definitions.conf /docker/rabbitmq-conf/definitions.conf
COPY docker/entrypoint.sh /docker/entrypoint.sh
COPY docker/entrypoint-parser.sh /docker/entrypoint-parser.sh

# Tell Pulumi to use the pre-built binary (skip go build at runtime)
RUN sed -i '/^  name: go$/a\  options:\n    binary: ./pulumi-ragflow' Pulumi.yaml && \
    echo "--- Pulumi.yaml runtime section ---" && head -6 Pulumi.yaml

# Verify
RUN pulumi version && pulumi plugin ls && ls -lh pulumi-ragflow

CMD ["/bin/bash"]
