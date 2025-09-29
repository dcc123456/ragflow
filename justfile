# https://github.com/casey/just
# uv tool install --python=3.10 rust-just

build-ragflow: check-clean
    cp -r agent api conf deepdoc docker rag web pyproject.toml uv.lock oss
    cd oss && docker build --build-arg NEED_MIRROR=1 --build-arg LIGHTEN=1 -t infiniflow-ai/ragflow:latest-slim .

check-clean:
    # check if the main repo is clean
    @if [ -n "$(git status --porcelain --ignored=no)" ]; then \
        echo "Error: Main Git repository is not clean."; \
        git status --short; \
        exit 1; \
    fi

    # check if submodule is clean
    @if git submodule status --recursive | grep -q '^+'; then \
        echo "Error: Some submodules have uncommitted changes (marked with '+')."; \
        git submodule status --recursive | grep '^+'; \
        exit 1; \
    fi

    @if git submodule foreach --recursive 'git status --porcelain --ignored=no' | grep -q '^[ M?]'; then \
        echo "Error: Some submodules have untracked or modified files."; \
        git submodule foreach --recursive 'git status --short'; \
        exit 1; \
    fi

build-model:
    cd deepdoc/servers/ocr && uv run download_deps.py && docker build -t infiniflow-ai/paddleocr .
    cd deepdoc/servers/embed && uv run download_deps.py && docker build -t infiniflow-ai/embed .
    cd deepdoc/servers/tsr && uv run download_deps.py && docker build -t infiniflow-ai/tsr .

sync:
    git submodule sync --recursive
    git submodule update --init --force --recursive
    git -C oss reset --hard
    git -C oss clean -ffdx
    cp -r agent api conf deepdoc docker rag web pyproject.toml uv.lock oss
    cd oss && uv sync --python 3.10 --all-extras --frozen

kill:
    pkill -f "ragflow_server.py|task_executor.py"

launch:
    cd oss && source .venv/bin/activate && export PYTHONPATH=$(pwd) && bash docker/launch_backend_service.sh
