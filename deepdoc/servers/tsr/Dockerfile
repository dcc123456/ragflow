FROM nvidia/cuda:12.9.0-cudnn-runtime-ubuntu22.04
ARG NEED_MIRROR=1

SHELL ["/bin/bash", "-c"]
ENV DEBIAN_FRONTEND=noninteractive

RUN --mount=type=cache,id=ragflow_apt,target=/var/cache/apt,sharing=locked \
    if [ "$NEED_MIRROR" == "1" ]; then \
        sed -i 's|http://archive.ubuntu.com|https://mirrors.tuna.tsinghua.edu.cn|g' /etc/apt/sources.list; \
    fi; \
    rm -f /etc/apt/apt.conf.d/docker-clean && \
    echo 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache && \
    chmod 1777 /tmp && \
    apt update && \
    apt --no-install-recommends install -y ca-certificates && \
    apt update && \
    apt install -y python3.10 python3-pip curl vim libgl1 libglib2.0-0

#RUN if [ "$NEED_MIRROR" == "1" ]; then \
#        pip3 config set global.index-url https://mirrors.aliyun.com/pypi/simple && \
#        pip3 config set global.trusted-host mirrors.aliyun.com; \
#        mkdir -p /etc/uv && \
#        echo "[[index]]" > /etc/uv/uv.toml && \
#        echo 'url = "https://mirrors.aliyun.com/pypi/simple"' >> /etc/uv/uv.toml && \
#        echo "default = true" >> /etc/uv/uv.toml; \
#    fi

# wget https://github.com/astral-sh/uv/releases/download/0.6.12/uv-x86_64-unknown-linux-gnu.tar.gz
RUN --mount=type=bind,source=/uv-x86_64-unknown-linux-gnu.tar.gz,target=/uv-x86_64-unknown-linux-gnu.tar.gz \
    tar xzvf uv-x86_64-unknown-linux-gnu.tar.gz && \
    mv uv-x86_64-unknown-linux-gnu/* /usr/local/bin/ && \
    rm -rf uv-x86_64-unknown-linux-gnu

WORKDIR /app

# Install TensorRT 10.9 GA for x86_64 Architecture
RUN --mount=type=bind,source=/nv-tensorrt-local-repo-ubuntu2204-10.12.0-cuda-12.9_1.0-1_amd64.deb,target=/nv-tensorrt-local-repo-ubuntu2204-10.12.0-cuda-12.9_1.0-1_amd64.deb \
    dpkg -i /nv-tensorrt-local-repo-ubuntu2204-10.12.0-cuda-12.9_1.0-1_amd64.deb && \
    cp /var/nv-tensorrt-local-repo-ubuntu2204-10.12.0-cuda-12.9/*-keyring.gpg /usr/share/keyrings/

# Install litserve and requirements
RUN --mount=type=cache,id=ragflow_uv,target=/root/.cache/uv,sharing=locked \
    uv venv && \
    uv pip install litserve==0.2.7 tensorrt onnx torch ultralytics opencv-python-headless numpy pillow setuptools python-multipart

COPY yolov8_to_tensorrt /app/yolov8_to_tensorrt
COPY tsr_svr.py tsr.trt /app
#COPY tsr_svr.py keepit.sh /app

ENV PYTHONPATH=/app
EXPOSE 1234
ENTRYPOINT ["/app/.venv/bin/python3", "/app/tsr_svr.py", "--engine=/app/tsr.trt"]
#ENTRYPOINT ["sh", "/app/keepit.sh"]
