FROM nvidia/cuda:12.8.0-cudnn-runtime-ubuntu22.04
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
    apt install -y python3.12 python3-pip curl vim libgl1 libglib2.0-0 wget unzip

#RUN if [ "$NEED_MIRROR" == "1" ]; then \
#        mkdir -p /etc/uv && \
#        echo "[[index]]" > /etc/uv/uv.toml && \
#        echo 'url = "https://mirrors.aliyun.com/pypi/simple"' >> /etc/uv/uv.toml && \
#        echo "default = true" >> /etc/uv/uv.toml; \
#    fi

# wget https://github.com/astral-sh/uv/releases/download/0.9.18/uv-x86_64-unknown-linux-gnu.tar.gz
RUN --mount=type=bind,source=/uv-x86_64-unknown-linux-gnu.tar.gz,target=/uv-x86_64-unknown-linux-gnu.tar.gz \
    tar xzvf uv-x86_64-unknown-linux-gnu.tar.gz && \
    mv uv-x86_64-unknown-linux-gnu/* /usr/local/bin/ && \
    rm -rf uv-x86_64-unknown-linux-gnu

WORKDIR /app

# Install TensorRT 10.8 for x86_64 Architecture
# CUDA 12.8 to match inf24 cluster (nvidia-smi shows CUDA 12.8)
# NOTE: Using TensorRT 10.8.0 as 10.12.0 doesn't have CUDA 12.8 build
RUN --mount=type=bind,source=/nv-tensorrt-local-repo-ubuntu2404-10.8.0-cuda-12.8_1.0-1_amd64.deb,target=/nv-tensorrt-local-repo-ubuntu2404-10.8.0-cuda-12.8_1.0-1_amd64.deb \
    dpkg -i /nv-tensorrt-local-repo-ubuntu2404-10.8.0-cuda-12.8_1.0-1_amd64.deb && \
    cp /var/nv-tensorrt-local-repo-ubuntu2404-10.8.0-cuda-12.8/*-keyring.gpg /usr/share/keyrings/

RUN  --mount=type=cache,id=ragflow_uv,target=/root/.cache/uv,sharing=locked \
    wget https://github.com/THU-MIG/yolov10/archive/refs/heads/main.zip && \
    mv main.zip yolov10.zip && unzip  yolov10.zip && cd yolov10-main && \
    uv venv && \
    uv pip install -r requirements.txt && \
    uv pip install -e ./

# Install litserve and requirements
RUN --mount=type=cache,id=ragflow_uv,target=/root/.cache/uv,sharing=locked \
    uv venv && \
    uv pip install litserve==0.2.7 tensorrt onnx torch ultralytics opencv-python-headless numpy pillow setuptools python-multipart cuda-python doclayout-yolo

COPY yolov10_to_tensor /app/yolov10_to_tensor
COPY dla_svr.py dla.trt /app
#COPY dla_svr.py keepit.sh /app

ENV PYTHONPATH=/app
EXPOSE 1234
ENTRYPOINT ["/app/.venv/bin/python3", "/app/dla_svr.py", "--engine=/app/dla.trt"]
#ENTRYPOINT ["sh", "/app/keepit.sh"]
