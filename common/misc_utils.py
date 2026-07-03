#
#  Copyright 2025 The InfiniFlow Authors. All Rights Reserved.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#
import asyncio
import base64
import contextvars
import functools
import hashlib
import logging
import os
import subprocess
import sys
from pathlib import Path
from typing import Dict
import threading
import uuid
from urllib.parse import urljoin

from concurrent.futures import ThreadPoolExecutor

logger = logging.getLogger(__name__)


def get_uuid():
    return uuid.uuid1().hex


# OAuth avatar fetch: bounded size; each redirect hop is SSRF-checked and DNS-pinned
# (see common.ssrf_guard).
_OAUTH_AVATAR_MAX_BYTES = int(os.environ.get("RAGFLOW_OAUTH_AVATAR_MAX_BYTES", str(5 * 1024 * 1024)))
_OAUTH_AVATAR_MAX_REDIRECTS = int(os.environ.get("RAGFLOW_OAUTH_AVATAR_MAX_REDIRECTS", "5"))
_REDIRECT_STATUS = frozenset({301, 302, 303, 307, 308})


async def download_img(url):
    """Fetch an image URL and return a data URI, or empty string on failure / SSRF block.

    URLs must resolve only to globally routable addresses; redirects are followed
    only up to ``_OAUTH_AVATAR_MAX_REDIRECTS`` with each target validated.
    """
    if not url:
        return ""
    if not isinstance(url, str):
        url = str(url)
    url = url.strip()
    if not url:
        return ""

    current_url = url
    redirect_hops = 0

    # Match common/http_client.py defaults without importing http_client (avoids
    # pulling settings and keeps this path usable in lightweight test envs).
    request_timeout = float(os.environ.get("HTTP_CLIENT_TIMEOUT", "15"))
    proxy = os.environ.get("HTTP_CLIENT_PROXY")
    user_agent = os.environ.get("HTTP_CLIENT_USER_AGENT", "ragflow-http-client")

    from common.ssrf_guard import assert_url_is_safe, pin_dns_global

    while redirect_hops <= _OAUTH_AVATAR_MAX_REDIRECTS:
        try:
            hostname, pin_ip = assert_url_is_safe(current_url)
        except ValueError as exc:
            logger.warning("download_img rejected URL (SSRF guard): %s", exc)
            return ""

        import httpx

        timeout = httpx.Timeout(request_timeout)
        headers = {}
        if user_agent:
            headers["User-Agent"] = user_agent

        async def _stream_one_get() -> tuple[str, str | None]:
            """Return ``('redirect', new_url)``, ``('data', data_uri)``, or ``('fail', None)``."""
            with pin_dns_global(hostname, pin_ip):
                async with httpx.AsyncClient(
                    timeout=timeout,
                    follow_redirects=False,
                    proxy=proxy,
                ) as client:
                    async with client.stream("GET", current_url, headers=headers or None) as response:
                        if response.status_code in _REDIRECT_STATUS:
                            await response.aclose()
                            location = response.headers.get("location")
                            if not location:
                                logger.warning(
                                    "download_img redirect missing Location header: status=%s redirect_hops=%s",
                                    response.status_code,
                                    redirect_hops,
                                )
                                return ("fail", None)
                            return ("redirect", urljoin(current_url, location))
                        if response.status_code != 200:
                            logger.warning(
                                "download_img non-200 response: status=%s redirect_hops=%s",
                                response.status_code,
                                redirect_hops,
                            )
                            return ("fail", None)
                        body = bytearray()
                        async for chunk in response.aiter_bytes():
                            if len(body) + len(chunk) > _OAUTH_AVATAR_MAX_BYTES:
                                logger.warning(
                                    # codeql[py/clear-text-logging-sensitive-data]
                                    # False positive: current_url was dropped
                                    # from the format args in this branch to
                                    # avoid leaking OAuth tokens embedded in
                                    # the URL query string. Only the static
                                    # threshold value is logged.
                                    "download_img response exceeded max size: max_bytes=%s",
                                    _OAUTH_AVATAR_MAX_BYTES,
                                )
                                await response.aclose()
                                return ("fail", None)
                            body.extend(chunk)
                        content_type = response.headers.get("Content-Type", "image/jpeg")
                        data_uri = "data:" + content_type + ";base64," + base64.b64encode(bytes(body)).decode("utf-8")
                        return ("data", data_uri)

        try:
            kind, payload = await asyncio.wait_for(_stream_one_get(), timeout=request_timeout)
        except asyncio.TimeoutError:
            logger.warning(
                "download_img total wall-clock timeout: redirect_hops=%s timeout=%s",
                redirect_hops,
                request_timeout,
            )
            return ""
        except Exception as exc:
            logger.warning(
                "download_img request failed: redirect_hops=%s err=%s",
                redirect_hops,
                exc,
            )
            return ""

        if kind == "redirect":
            current_url = str(payload)
            redirect_hops += 1
            continue
        if kind == "fail":
            return ""
        return str(payload)

    # codeql[py/clear-text-logging-sensitive-data]
    # False positive: current_url was already dropped from the format
    # args in this branch to avoid leaking OAuth tokens. Only the
    # hop count and configured max are logged.
    logger.warning(
        "download_img redirect hop limit exceeded: redirect_hops=%s max_redirects=%s",
        redirect_hops,
        _OAUTH_AVATAR_MAX_REDIRECTS,
    )
    return ""


def hash_str2int(line: str, mod: int = 10**8) -> int:
    return int(hashlib.sha1(line.encode("utf-8")).hexdigest(), 16) % mod


def convert_bytes(size_in_bytes: int) -> str:
    """
    Format size in bytes.
    """
    if size_in_bytes == 0:
        return "0 B"

    units = ["B", "KB", "MB", "GB", "TB", "PB"]
    i = 0
    size = float(size_in_bytes)

    while size >= 1024 and i < len(units) - 1:
        size /= 1024
        i += 1

    if i == 0 or size >= 100:
        return f"{size:.0f} {units[i]}"
    elif size >= 10:
        return f"{size:.1f} {units[i]}"
    else:
        return f"{size:.2f} {units[i]}"


def once(func):
    """
    A thread-safe decorator that ensures the decorated function runs exactly once,
    caching and returning its result for all subsequent calls. This prevents
    race conditions in multi-thread environments by using a lock to protect
    the execution state.

    Args:
        func (callable): The function to be executed only once.

    Returns:
        callable: A wrapper function that executes `func` on the first call
                  and returns the cached result thereafter.

    Example:
        @once
        def compute_expensive_value():
            print("Computing...")
            return 42

        # First call: executes and prints
        # Subsequent calls: return 42 without executing
    """
    executed = False
    result = None
    lock = threading.Lock()

    def wrapper(*args, **kwargs):
        nonlocal executed, result
        with lock:
            if not executed:
                executed = True
                result = func(*args, **kwargs)
        return result

    return wrapper


@once
def pip_install_torch():
    device = os.getenv("DEVICE", "cpu")
    if device == "cpu":
        return
    logging.info("Installing pytorch")
    pkg_names = ["torch>=2.5.0,<3.0.0"]
    subprocess.check_call([sys.executable, "-m", "pip", "install", *pkg_names])


def parse_mineru_paths() -> Dict[str, Path]:
    """
    Parse MinerU-related paths based on the MINERU_EXECUTABLE environment variable.

    Expected layout (default convention):
        MINERU_EXECUTABLE = /home/user/uv_tools/.venv/bin/mineru

    From this path we derive:
        - mineru_exec : full path to the mineru executable
        - venv_dir    : the virtual environment directory (.venv)
        - tools_dir   : the parent tools directory (e.g. uv_tools)

    If MINERU_EXECUTABLE is not set, we fall back to the default layout:
        $HOME/uv_tools/.venv/bin/mineru

    Returns:
        A dict with keys:
            - "mineru_exec": Path
            - "venv_dir": Path
            - "tools_dir": Path
    """
    mineru_exec_env = os.getenv("MINERU_EXECUTABLE")

    if mineru_exec_env:
        # Use the path from the environment variable
        mineru_exec = Path(mineru_exec_env).expanduser().resolve()
        venv_dir = mineru_exec.parent.parent
        tools_dir = venv_dir.parent
    else:
        # Fall back to default convention: $HOME/uv_tools/.venv/bin/mineru
        home = Path(os.path.expanduser("~"))
        tools_dir = home / "uv_tools"
        venv_dir = tools_dir / ".venv"
        mineru_exec = venv_dir / "bin" / "mineru"

    return {
        "mineru_exec": mineru_exec,
        "venv_dir": venv_dir,
        "tools_dir": tools_dir,
    }


@once
def check_and_install_mineru() -> None:
    """
    Ensure MinerU is installed.

    Behavior:
      1. MinerU is enabled only when USE_MINERU is true/yes/1/y.
      2. Resolve mineru_exec / venv_dir / tools_dir.
      3. If mineru exists and works, log success and exit.
      4. Otherwise:
          - Create tools_dir
          - Create venv if missing
          - Install mineru[core], fallback to mineru[all]
          - Validate with `--help`
      5. Log installation success.

    NOTE:
        This function intentionally does NOT return the path.
        Logging is used to indicate status.
    """
    # Check if MinerU is enabled
    use_mineru = os.getenv("USE_MINERU", "false").strip().lower()
    if use_mineru != "true":
        logging.info("USE_MINERU=%r. Skipping MinerU installation.", use_mineru)
        return

    # Resolve expected paths
    paths = parse_mineru_paths()
    mineru_exec: Path = paths["mineru_exec"]
    venv_dir: Path = paths["venv_dir"]
    tools_dir: Path = paths["tools_dir"]

    # Construct environment variables for installation/execution
    env = os.environ.copy()
    env["VIRTUAL_ENV"] = str(venv_dir)
    env["PATH"] = str(venv_dir / "bin") + os.pathsep + env.get("PATH", "")

    # Configure HuggingFace endpoint
    env.setdefault("HUGGINGFACE_HUB_ENDPOINT", os.getenv("HF_ENDPOINT") or "https://hf-mirror.com")

    # Helper: check whether mineru works
    def mineru_works() -> bool:
        try:
            subprocess.check_call(
                [str(mineru_exec), "--help"],
                stdout=subprocess.DEVNULL,
                stderr=subprocess.PIPE,
                env=env,
            )
            return True
        except Exception:
            return False

    # If MinerU is already installed and functional
    if mineru_exec.is_file() and os.access(mineru_exec, os.X_OK) and mineru_works():
        logging.info("MinerU already  installed.")
        os.environ["MINERU_EXECUTABLE"] = str(mineru_exec)
        return

    logging.info("MinerU not found. Installing into virtualenv: %s", venv_dir)

    # Ensure parent directory exists
    tools_dir.mkdir(parents=True, exist_ok=True)

    # Create venv if missing
    if not venv_dir.exists():
        subprocess.check_call(
            ["uv", "venv", str(venv_dir)],
            cwd=str(tools_dir),
            env=env,
            # stdout=subprocess.DEVNULL,
            # stderr=subprocess.PIPE,
        )
    else:
        logging.info("Virtual environment exists at %s. Reusing it.", venv_dir)

    # Helper for pip install
    def pip_install(pkg: str) -> None:
        subprocess.check_call(
            [
                "uv",
                "pip",
                "install",
                "-U",
                pkg,
                "-i",
                "https://mirrors.aliyun.com/pypi/simple",
                "--extra-index-url",
                "https://pypi.org/simple",
            ],
            cwd=str(tools_dir),
            # stdout=subprocess.DEVNULL,
            # stderr=subprocess.PIPE,
            env=env,
        )

    # Install core version first; fallback to all
    try:
        logging.info("Installing mineru[core] ...")
        pip_install("mineru[core]")
    except subprocess.CalledProcessError:
        logging.warning("mineru[core] installation failed. Installing mineru[all] ...")
        pip_install("mineru[all]")

    # Validate installation
    if not mineru_works():
        logging.error("MinerU installation failed: %s does not work.", mineru_exec)
        raise RuntimeError(f"MinerU installation failed: {mineru_exec} is not functional")

    os.environ["MINERU_EXECUTABLE"] = str(mineru_exec)
    logging.info("MinerU installation completed successfully. Executable: %s", mineru_exec)


@once
def _thread_pool_executor():
    max_workers_env = os.getenv("THREAD_POOL_MAX_WORKERS", "128")
    try:
        max_workers = int(max_workers_env)
    except ValueError:
        max_workers = 128
    if max_workers < 1:
        max_workers = 1
    return ThreadPoolExecutor(max_workers=max_workers)


async def thread_pool_exec(func, *args, **kwargs):
    # loop.run_in_executor() submits the callable without propagating the caller's
    # contextvars (unlike asyncio.to_thread, which copies the context). Copy the
    # current context and run the callable inside it so ContextVars set by the
    # caller (e.g. tracing / per-request state) are visible in the worker thread.
    loop = asyncio.get_running_loop()
    ctx = contextvars.copy_context()
    if kwargs:
        inner = functools.partial(func, *args, **kwargs)
        return await loop.run_in_executor(_thread_pool_executor(), ctx.run, inner)
    return await loop.run_in_executor(_thread_pool_executor(), ctx.run, func, *args)
