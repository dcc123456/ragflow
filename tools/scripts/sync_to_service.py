#!/usr/bin/env python3
"""Sync code changes to a running RagFlow service (k8s pod or Docker container).

Usage:
    python sync_to_service.py                  # Interactive: auto-detect and choose service
    python sync_to_service.py --pod POD_NAME   # Sync to specific k8s pod
    python sync_to_service.py --container NAME # Sync to specific Docker container
    python sync_to_service.py --python-only    # Skip npm build + dist sync
    python sync_to_service.py --dist-only      # Only sync dist (after manual npm build)
    python sync_to_service.py --direct-docker-rootfs
                                             # Docker only: rsync directly into container rootfs

Prereq (target service):
    - rsync installed (apt install rsync / apk add rsync)
    - kubectl configured for k8s OR docker CLI for container mode
"""

import os
import subprocess
import time
import sys
import argparse
import socket
import shlex
import re
import selectors
from pathlib import Path
from dataclasses import dataclass
from typing import Optional


# ---------------------------------------------------------------------------
# Deployment target abstraction
# ---------------------------------------------------------------------------


@dataclass
class DeploymentTarget:
    """Abstract deployment target (k8s pod or Docker container)."""

    name: str
    kind: str  # "k8s" or "docker"
    namespace: str = "ragflow"

    def exec(self, *args: str, check: bool = True) -> subprocess.CompletedProcess:
        raise NotImplementedError

    def exec_stream(self, *args: str) -> subprocess.Popen:
        raise NotImplementedError

    def exec_input(self, *args: str, input_text: str, check: bool = True) -> subprocess.CompletedProcess:
        raise NotImplementedError

    def file_exists(self, path: str) -> bool:
        raise NotImplementedError


class K8sTarget(DeploymentTarget):
    kind = "k8s"

    def __init__(self, name: str, namespace: str = "ragflow"):
        super().__init__(name, "k8s", namespace)

    def exec(self, *args: str, check: bool = True) -> subprocess.CompletedProcess:
        cmd = ["kubectl", "exec", "-i", self.name, "-n", self.namespace, "--"] + list(args)
        result = subprocess.run(cmd, capture_output=True, text=True)
        if check and result.returncode != 0:
            raise RuntimeError(f"kubectl exec failed: {result.stderr}")
        return result

    def exec_stream(self, *args: str) -> subprocess.Popen:
        cmd = ["kubectl", "exec", "-i", self.name, "-n", self.namespace, "--"] + list(args)
        return subprocess.Popen(cmd, stdin=subprocess.PIPE)

    def exec_input(self, *args: str, input_text: str, check: bool = True) -> subprocess.CompletedProcess:
        cmd = ["kubectl", "exec", "-i", self.name, "-n", self.namespace, "--"] + list(args)
        result = subprocess.run(cmd, input=input_text, capture_output=True, text=True)
        if check and result.returncode != 0:
            raise RuntimeError(f"kubectl exec failed: {result.stderr}")
        return result

    def file_exists(self, path: str) -> bool:
        result = subprocess.run(["kubectl", "exec", self.name, "-n", self.namespace, "--", "test", "-e", path], capture_output=True)
        return result.returncode == 0


class DockerTarget(DeploymentTarget):
    kind = "docker"

    def __init__(self, name: str):
        super().__init__(name, "docker", "")

    def exec(self, *args: str, check: bool = True) -> subprocess.CompletedProcess:
        cmd = ["docker", "exec", "-i", self.name] + list(args)
        result = subprocess.run(cmd, capture_output=True, text=True)
        if check and result.returncode != 0:
            raise RuntimeError(f"docker exec failed: {result.stderr}")
        return result

    def exec_stream(self, *args: str) -> subprocess.Popen:
        cmd = ["docker", "exec", "-i", self.name] + list(args)
        return subprocess.Popen(cmd, stdin=subprocess.PIPE)

    def exec_input(self, *args: str, input_text: str, check: bool = True) -> subprocess.CompletedProcess:
        cmd = ["docker", "exec", "-i", self.name] + list(args)
        result = subprocess.run(cmd, input=input_text, capture_output=True, text=True)
        if check and result.returncode != 0:
            raise RuntimeError(f"docker exec failed: {result.stderr}")
        return result

    def file_exists(self, path: str) -> bool:
        result = subprocess.run(["docker", "exec", self.name, "test", "-e", path], capture_output=True)
        return result.returncode == 0


# ---------------------------------------------------------------------------
# Target discovery
# ---------------------------------------------------------------------------


def find_k8s_pods(namespace: str = "ragflow") -> list[K8sTarget]:
    """Return list of K8sTarget for ragflow pods."""
    result = subprocess.run(["kubectl", "get", "pods", "-n", namespace, "-l", "app=ragflow", "-o", "jsonpath={.items[*].metadata.name}"], capture_output=True, text=True)
    names = result.stdout.strip().split()
    if not names:
        return []
    return [K8sTarget(name, namespace) for name in names]


def find_docker_containers() -> list[DockerTarget]:
    """Return list of DockerTarget for docker-ragflow-* containers."""
    result = subprocess.run(["docker", "ps", "--format", "{{.Names}}"], capture_output=True, text=True)
    if result.returncode != 0:
        return []
    names = [n.strip() for n in result.stdout.strip().split("\n") if n.strip()]
    ragflow_names = [n for n in names if n.startswith("docker-ragflow")]
    return [DockerTarget(name) for name in ragflow_names]


def detect_environment() -> str:
    """Detect whether we're in k8s or docker mode."""
    # Check kubectl
    result = subprocess.run(["kubectl", "cluster-info"], capture_output=True, text=True, timeout=5)
    if result.returncode == 0:
        # Check if there are any ragflow pods
        if find_k8s_pods():
            return "k8s"
    # Check docker
    if find_docker_containers():
        return "docker"
    return "none"


def pick_target(targets: list[DeploymentTarget], kind: str) -> DeploymentTarget:
    """Interactively pick a target, or auto-pick the first Running one."""
    if not targets:
        raise RuntimeError(f"No {kind} targets found")

    if len(targets) == 1:
        return targets[0]

    print(f"\nAvailable {kind} targets:")
    for i, t in enumerate(targets, 1):
        if kind == "k8s":
            # Get pod status
            phase = subprocess.run(["kubectl", "get", "pod", t.name, "-n", t.namespace, "-o", "jsonpath={.status.phase}"], capture_output=True, text=True).stdout.strip()
            marker = " (Running)" if phase == "Running" else f" ({phase})"
        else:
            # Get container status
            status = subprocess.run(["docker", "ps", "--filter", f"name={t.name}", "--format", "{{.Status}}"], capture_output=True, text=True).stdout.strip()
            marker = f" ({status})"
        print(f"  {i}. {t.name}{marker}")
    print()

    while True:
        try:
            choice = input(f"Select {kind} [1-{len(targets)}] or press Enter for default (1): ").strip()
            if choice == "":
                return targets[0]
            idx = int(choice) - 1
            if 0 <= idx < len(targets):
                return targets[idx]
        except (ValueError, EOFError):
            pass
        print("Invalid selection.")


# ---------------------------------------------------------------------------
# rsyncd setup
# ---------------------------------------------------------------------------

RSYNC_PORT = 8873


def ensure_rsyncd_conf(target: DeploymentTarget, remote_port: int = RSYNC_PORT):
    """Ensure /etc/rsyncd.conf exists in target with a [ragflow] module."""
    # Bind to 0.0.0.0 so socat/docker can reach it via container IP
    content = f"""uid = root
gid = root
port = {remote_port}
address = 0.0.0.0
[ragflow]
    path = /ragflow
    read only = no
    list = yes
"""
    result = target.exec_input("sh", "-c", "cat > /etc/rsyncd.conf", input_text=content, check=False)
    if result.returncode != 0:
        details = (result.stderr or result.stdout or "no output").strip()
        raise RuntimeError(f"Failed to write /etc/rsyncd.conf in {target.kind} {target.name}: {details}")


def start_rsync_daemon(target: DeploymentTarget, remote_port: int = RSYNC_PORT):
    """Kill any existing rsyncd, write config, start fresh daemon."""
    ensure_rsyncd_conf(target, remote_port)

    last_start = None
    last_check = None
    for attempt in range(1, 4):
        target.exec("sh", "-c", "pkill -9 rsync 2>/dev/null; true", check=False)
        time.sleep(0.5)

        last_start = target.exec(
            "sh",
            "-c",
            f"nohup rsync --daemon --no-detach --port={remote_port} --config=/etc/rsyncd.conf > /tmp/rsyncd.log 2>&1 &",
            check=False,
        )
        last_check = wait_for_rsync_module(target, remote_port)
        if last_check.returncode == 0 and "ragflow" in last_check.stdout:
            print(f"  rsyncd running in {target.kind} {target.name}")
            return
        if attempt < 3:
            print(f"  rsyncd not ready yet, retrying ({attempt}/3) ...")

    log = target.exec("sh", "-c", "cat /tmp/rsyncd.log 2>/dev/null || echo '(no log)'").stdout.strip()
    start_details = ""
    if last_start is not None and (last_start.stderr or last_start.stdout):
        start_details = f" Start: {(last_start.stderr or last_start.stdout).strip()}."
    check_details = "no output"
    if last_check is not None:
        check_details = (last_check.stderr or last_check.stdout or "no output").strip()
    raise RuntimeError(f"rsyncd failed to start.{start_details} Check: {check_details}. Log: {log}")


# ---------------------------------------------------------------------------
# port-forward (k8s) / docker port (docker)
# ---------------------------------------------------------------------------

_k8s_pf_proc = None
_docker_port_map: dict[int, int] = {}  # local_port -> container_port


def start_k8s_port_forward(target: K8sTarget, local_port: int = RSYNC_PORT, remote_port: int = RSYNC_PORT):
    """Start kubectl port-forward in background."""
    global _k8s_pf_proc

    # Kill any old port-forward on that port
    subprocess.run(["pkill", "-f", f"kubectl.*port-forward.*{local_port}:{remote_port}"], capture_output=True)
    time.sleep(0.5)

    pf_proc = subprocess.Popen(["kubectl", "port-forward", "-n", target.namespace, target.name, f"{local_port}:{remote_port}"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    _k8s_pf_proc = pf_proc
    time.sleep(3)

    if pf_proc.poll() is not None:
        raise RuntimeError("port-forward failed to start")

    print(f"  kubectl port-forward active: localhost:{local_port} -> {target.name}:{remote_port}")
    return pf_proc


def get_docker_container_ip(target: DockerTarget) -> str:
    """Get the internal IP address of a Docker container."""
    result = subprocess.run(["docker", "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", target.name], capture_output=True, text=True)
    container_ip = result.stdout.strip()
    if not container_ip:
        raise RuntimeError(f"Could not get IP address for container {target.name}")
    return container_ip


def get_docker_rootfs(target: DockerTarget) -> Path:
    """Return Docker's merged rootfs path for a local container."""
    result = subprocess.run(
        ["docker", "inspect", "-f", "{{.GraphDriver.Data.MergedDir}}", target.name],
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError(f"Could not inspect Docker rootfs for {target.name}: {result.stderr}")
    merged_dir = result.stdout.strip()
    if not merged_dir:
        raise RuntimeError(f"Docker did not report GraphDriver.Data.MergedDir for {target.name}")
    rootfs = Path(merged_dir)
    if not str(rootfs).startswith("/var/lib/docker/"):
        raise RuntimeError(f"Refusing direct rootfs sync outside /var/lib/docker: {rootfs}")
    ragflow_dir = rootfs / "ragflow"
    result = subprocess.run(["sudo", "-n", "test", "-d", str(ragflow_dir)], capture_output=True, text=True)
    if result.returncode != 0:
        raise RuntimeError(f"Container rootfs does not contain /ragflow or sudo is unavailable: {ragflow_dir}")
    return rootfs


def start_docker_port_forward(target: DockerTarget, local_port: int = RSYNC_PORT, remote_port: int = RSYNC_PORT):
    """Docker mode: container IP is directly reachable, no port-forward needed."""
    global _docker_port_map
    container_ip = get_docker_container_ip(target)
    _docker_port_map[local_port] = remote_port  # Docker mode uses container_ip:port directly
    print(f"  docker rsync target: {container_ip}:{remote_port}")


def stop_port_forward():
    global _k8s_pf_proc, _docker_port_map
    if _k8s_pf_proc is not None:
        _k8s_pf_proc.terminate()
        _k8s_pf_proc = None
    # Clean up docker port forward containers
    for local_port in list(_docker_port_map.keys()):
        subprocess.run(["docker", "rm", "-f", f"_sync_port_forward_{local_port}"], capture_output=True)
    _docker_port_map.clear()


def is_port_open(port: int, timeout: float = 2) -> bool:
    try:
        with socket.create_connection(("localhost", port), timeout=timeout):
            return True
    except (socket.timeout, ConnectionRefusedError, OSError):
        return False


def wait_for_port(port: int, timeout: float = 10) -> bool:
    deadline = time.time() + timeout
    while time.time() < deadline:
        if is_port_open(port):
            return True
        time.sleep(0.5)
    return False


def wait_for_rsync_module(target: DeploymentTarget, remote_port: int, timeout: float = 10) -> subprocess.CompletedProcess:
    """Wait until rsyncd lists the ragflow module from inside the target."""
    deadline = time.time() + timeout
    last_result = target.exec("rsync", f"rsync://127.0.0.1:{remote_port}/", check=False)
    while time.time() < deadline:
        if last_result.returncode == 0 and "ragflow" in last_result.stdout:
            return last_result
        time.sleep(0.5)
        last_result = target.exec("rsync", f"rsync://127.0.0.1:{remote_port}/", check=False)
    return last_result


# ---------------------------------------------------------------------------
# Repository root
# ---------------------------------------------------------------------------

_REPO_ROOT = Path(__file__).resolve().parent.parent.parent
_SYNC_LOG_DIR = _REPO_ROOT / ".sync_logs"


# ---------------------------------------------------------------------------
# Build and sync
# ---------------------------------------------------------------------------


def _write_command_log(step_name: str, result: subprocess.CompletedProcess) -> Path:
    _SYNC_LOG_DIR.mkdir(parents=True, exist_ok=True)
    safe_step_name = re.sub(r"[^a-zA-Z0-9_.-]+", "_", step_name).strip("_") or "command"
    log_path = _SYNC_LOG_DIR / f"{int(time.time())}_{safe_step_name}.log"
    stdout = result.stdout or ""
    stderr = result.stderr or ""
    content = f"step: {step_name}\nreturncode: {result.returncode}\n{'-' * 80}\n[stdout]\n{stdout}\n{'-' * 80}\n[stderr]\n{stderr}\n"
    log_path.write_text(content)
    return log_path


def _print_command_failure(step_name: str, result: subprocess.CompletedProcess):
    log_path = _write_command_log(step_name, result)
    print(f"  [ERROR] {step_name} failed with exit code {result.returncode}")
    print(f"  [ERROR] Full log written to: {log_path}")
    stdout = (result.stdout or "").strip()
    stderr = (result.stderr or "").strip()
    if stdout:
        print("  [STDOUT]")
        print(stdout)
    if stderr:
        print("  [STDERR]")
        print(stderr)


def _run_streaming_command(step_name: str, cmd: list[str], cwd: Path, env: Optional[dict[str, str]] = None) -> subprocess.CompletedProcess:
    _SYNC_LOG_DIR.mkdir(parents=True, exist_ok=True)
    safe_step_name = re.sub(r"[^a-zA-Z0-9_.-]+", "_", step_name).strip("_") or "command"
    log_path = _SYNC_LOG_DIR / f"{int(time.time())}_{safe_step_name}.log"
    print(f"  Streaming logs to: {log_path}")

    proc = subprocess.Popen(
        cmd,
        cwd=cwd,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        bufsize=1,
    )

    selector = selectors.DefaultSelector()
    assert proc.stdout is not None
    assert proc.stderr is not None
    selector.register(proc.stdout, selectors.EVENT_READ, "stdout")
    selector.register(proc.stderr, selectors.EVENT_READ, "stderr")

    stdout_chunks: list[str] = []
    stderr_chunks: list[str] = []

    with log_path.open("w") as log_file:
        log_file.write(f"step: {step_name}\n")
        log_file.write(f"command: {shlex.join(cmd)}\n")
        log_file.write(f"cwd: {cwd}\n")
        log_file.write(f"{'-' * 80}\n")
        log_file.flush()

        while selector.get_map():
            for key, _ in selector.select():
                stream = key.fileobj
                source = key.data
                line = stream.readline()
                if line == "":
                    selector.unregister(stream)
                    continue

                if source == "stdout":
                    stdout_chunks.append(line)
                    print(line, end="")
                    log_file.write(f"[stdout] {line}")
                else:
                    stderr_chunks.append(line)
                    print(line, end="", file=sys.stderr)
                    log_file.write(f"[stderr] {line}")
                log_file.flush()

        return_code = proc.wait()
        log_file.write(f"{'-' * 80}\n")
        log_file.write(f"returncode: {return_code}\n")

    return subprocess.CompletedProcess(
        cmd,
        return_code,
        "".join(stdout_chunks),
        "".join(stderr_chunks),
    )


def run_npm_build():
    """Run npm install && npm run build in the web/ directory."""
    web_dir = _REPO_ROOT / "web"
    if not web_dir.exists():
        print("  [WARN] web/ directory not found, skipping frontend build")
        return False

    node_build_env = {
        **os.environ,
        "NODE_OPTIONS": "--max-old-space-size=8192",
    }

    print("  Running npm install ...")
    result = _run_streaming_command(
        "npm install",
        ["npm", "install"],
        cwd=web_dir,
        env=node_build_env,
    )
    if result.returncode != 0:
        _print_command_failure("npm install", result)
        return False

    print("  Running npm run build ...")
    result = _run_streaming_command(
        "npm run build",
        ["npm", "run", "build"],
        cwd=web_dir,
        env={
            **node_build_env,
            "VITE_BUILD_SOURCEMAP": "false",
        },
    )
    if result.returncode != 0:
        _print_command_failure("npm run build", result)
        return False

    dist_dir = web_dir / "dist"
    if not dist_dir.exists():
        print("  [WARN] web/dist/ was not created, skipping dist sync")
        return False

    print("  frontend built at web/dist/")
    return True


RSYNC_EXCLUDES = [
    "--exclude=**/__pycache__/",
    "--exclude=**/.venv/",
    "--exclude=**/.git/",
    "--exclude=**/node_modules/",
    "--exclude=**/.pytest_cache/",
    "--exclude=**/.ruff_cache/",
    "--exclude=**/*.pyc",
    "--exclude=**/.DS_Store",
    "--exclude=**/.emdash/",
]


def sync_python_files(target: DeploymentTarget, local_port: int = RSYNC_PORT):
    """rsync all *.py files from repo root to target's /ragflow."""
    if target.kind == "docker":
        container_ip = get_docker_container_ip(target)
        dest = f"rsync://{container_ip}:{local_port}/ragflow"
    else:
        dest = f"rsync://localhost:{local_port}/ragflow"

    print("  Syncing Python files to target ...")
    cmd = [
        "rsync",
        "-avz",
        "--progress",
        *RSYNC_EXCLUDES,
        "--include=*/",
        "--include=**/*.py",
        "--exclude=*",
        str(_REPO_ROOT) + "/",
        dest + "/",
    ]
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode != 0:
        print(f"  [ERROR] command: {shlex.join(cmd)}")
        print(f"  [ERROR] rsync failed:\n{result.stderr[-500:]}")
        return False
    lines = result.stdout.strip().split("\n")
    for line in lines[-5:]:
        print(f"    {line}")
    return True


def sync_python_files_direct(rootfs: Path):
    """rsync all *.py files from repo root directly into a Docker container rootfs."""
    dest = rootfs / "ragflow"
    print(f"  Syncing Python files directly to {dest} ...")
    cmd = [
        "sudo",
        "-n",
        "rsync",
        "-avz",
        "--progress",
        *RSYNC_EXCLUDES,
        "--include=*/",
        "--include=**/*.py",
        "--exclude=*",
        str(_REPO_ROOT) + "/",
        str(dest) + "/",
    ]
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode != 0:
        print(f"  [ERROR] command: {shlex.join(cmd)}")
        print(f"  [ERROR] rsync failed:\n{result.stderr[-500:]}")
        return False
    lines = result.stdout.strip().split("\n")
    for line in lines[-5:]:
        print(f"    {line}")
    return True


def sync_dist_files(target: DeploymentTarget, local_port: int = RSYNC_PORT):
    """rsync web/dist/ to target's /ragflow/web/dist/."""
    dist_dir = _REPO_ROOT / "web" / "dist"

    if not dist_dir.exists():
        print("  [SKIP] web/dist/ does not exist (run with --all to include build)")
        return True

    if target.kind == "docker":
        container_ip = get_docker_container_ip(target)
        dest = f"rsync://{container_ip}:{local_port}/ragflow/web/dist"
    else:
        dest = f"rsync://localhost:{local_port}/ragflow/web/dist"
    print("  Syncing web/dist/ to target ...")

    cmd = [
        "rsync",
        "-avz",
        "--progress",
        str(dist_dir) + "/",
        dest + "/",
    ]
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode != 0:
        print(f"  [ERROR] command: {shlex.join(cmd)}")
        print(f"  [ERROR] rsync failed:\n{result.stderr[-500:]}")
        return False
    lines = result.stdout.strip().split("\n")
    for line in lines[-5:]:
        print(f"    {line}")
    return True


def sync_dist_files_direct(rootfs: Path):
    """rsync web/dist/ directly into a Docker container rootfs."""
    dist_dir = _REPO_ROOT / "web" / "dist"

    if not dist_dir.exists():
        print("  [SKIP] web/dist/ does not exist (run without --no-build to create it)")
        return True

    dest = rootfs / "ragflow" / "web" / "dist"
    print(f"  Syncing web/dist/ directly to {dest} ...")
    cmd = [
        "sudo",
        "-n",
        "rsync",
        "-avz",
        "--progress",
        str(dist_dir) + "/",
        str(dest) + "/",
    ]
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode != 0:
        print(f"  [ERROR] command: {shlex.join(cmd)}")
        print(f"  [ERROR] rsync failed:\n{result.stderr[-500:]}")
        return False
    lines = result.stdout.strip().split("\n")
    for line in lines[-5:]:
        print(f"    {line}")
    return True


# ---------------------------------------------------------------------------
# Process management
# ---------------------------------------------------------------------------

ENTRYPOINT_PYTHON_SCRIPTS = [
    "api/ragflow_server.py",
    "admin/server/admin_server.py",
    "rag/svr/sync_data_source.py",
]


def kill_ragflow_processes(target: DeploymentTarget):
    """Kill entrypoint-managed Python processes in the target to trigger restart."""
    if target.kind == "k8s":
        ps_command = ["kubectl", "exec", target.name, "-n", target.namespace, "--", "bash", "-lc", "ps -auxf"]
        list_command = f"pgrep -f {shlex.quote('|'.join(ENTRYPOINT_PYTHON_SCRIPTS))}"
        list_process_command = ["kubectl", "exec", target.name, "-n", target.namespace, "--", "bash", "-lc", list_command]
        kill_prefix = ["kubectl", "exec", target.name, "-n", target.namespace, "--", "bash", "-lc"]
    else:  # docker
        ps_command = ["docker", "exec", target.name, "bash", "-lc", "ps -auxf"]
        list_command = f"pgrep -f {shlex.quote('|'.join(ENTRYPOINT_PYTHON_SCRIPTS))}"
        list_process_command = ["docker", "exec", target.name, "bash", "-lc", list_command]
        kill_prefix = ["docker", "exec", target.name, "bash", "-lc"]

    print(f"  process snapshot before kill: {shlex.join(ps_command)}")
    ps_before = subprocess.run(ps_command, capture_output=True, text=True)
    if ps_before.returncode == 0:
        print(ps_before.stdout.rstrip())
    else:
        print(f"  [WARN] Failed to capture process snapshot: {ps_before.stderr[-300:]}")

    list_result = subprocess.run(list_process_command, capture_output=True, text=True)
    if list_result.returncode not in (0, 1):
        print(f"  [WARN] Failed to list processes: {list_result.stderr[-300:]}")
        return

    processes = []
    for line in list_result.stdout.strip().splitlines():
        pid = line.strip()
        if not pid:
            continue
        if target.kind == "k8s":
            describe_command = ["kubectl", "exec", target.name, "-n", target.namespace, "--", "bash", "-lc", f"ps -p {shlex.quote(pid)} -o args="]
        else:
            describe_command = ["docker", "exec", target.name, "bash", "-lc", f"ps -p {shlex.quote(pid)} -o args="]
        describe_result = subprocess.run(describe_command, capture_output=True, text=True)
        command = describe_result.stdout.strip() if describe_result.returncode == 0 else ""
        processes.append((pid, command))

    if not processes:
        print("  no entrypoint-managed Python processes found")
        return

    for pid, command in processes:
        kill_command = kill_prefix + [f"kill -TERM {shlex.quote(pid)}"]
        print(f"  killing pid={pid}: {command}")
        result = subprocess.run(kill_command, capture_output=True, text=True)
        if result.returncode not in (0, 143, 137):
            print(f"  [WARN] Failed to signal pid={pid}: {result.stderr[-300:]}")
        else:
            print(f"  pid={pid} signaled for restart")

    print("  process snapshot after kill:")
    ps_after = subprocess.run(ps_command, capture_output=True, text=True)
    if ps_after.returncode == 0:
        print(ps_after.stdout.rstrip())


def reload_nginx(target: DeploymentTarget):
    """Send SIGHUP to nginx master to reload static assets after dist sync."""
    if target.kind == "k8s":
        cmd = ["kubectl", "exec", target.name, "-n", target.namespace, "--", "sh", "-c", "pkill -HUP -f 'nginx: master' || true"]
    else:
        cmd = ["docker", "exec", target.name, "sh", "-c", "pkill -HUP -f 'nginx: master' || true"]
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode in (0, 129, 143, 137):
        print("  nginx reloaded (SIGHUP sent)")
    else:
        print(f"  [WARN] Failed to reload nginx: {result.stderr[-200:]}")


def restart_container(target: DockerTarget):
    """Restart a Docker container."""
    print(f"  Restarting container {target.name} ...")
    result = subprocess.run(["docker", "restart", target.name], capture_output=True, text=True)
    if result.returncode != 0:
        raise RuntimeError(f"Failed to restart container: {result.stderr}")
    print(f"  Container {target.name} restarted")


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------


def main():
    parser = argparse.ArgumentParser(description="Sync code changes to a running RagFlow service (k8s or Docker).")
    parser.add_argument("-n", "--namespace", default="ragflow", help="K8s namespace (default: ragflow)")
    parser.add_argument("-p", "--pod", dest="pod_name", help="Specific k8s pod name (skips selection prompt)")
    parser.add_argument("-c", "--container", dest="container_name", help="Specific Docker container name (skips selection prompt)")
    parser.add_argument("--python-only", action="store_true", help="Only sync Python files, skip npm build and dist")
    parser.add_argument("--dist-only", action="store_true", help="Only sync web/dist (assumes build already done)")
    parser.add_argument("--build", action="store_true", default=True, help="Run npm build before syncing (default: True)")
    parser.add_argument("--no-build", dest="build", action="store_false", help="Skip npm build")
    parser.add_argument("--direct-docker-rootfs", action="store_true", help="Docker only: rsync directly into Docker's local merged rootfs using sudo")
    parser.add_argument("-l", "--local-port", type=int, default=RSYNC_PORT, help=f"Local rsync port (default: {RSYNC_PORT})")
    parser.add_argument("-r", "--remote-port", type=int, default=RSYNC_PORT, help=f"Remote rsync port (default: {RSYNC_PORT})")
    args = parser.parse_args()

    namespace = args.namespace
    local_port = args.local_port
    remote_port = args.remote_port
    needs_dist_sync = not args.python_only
    needs_python_sync = not args.dist_only
    use_direct_rootfs = args.direct_docker_rootfs
    needs_rsync = (needs_dist_sync or needs_python_sync) and not use_direct_rootfs
    step_no = 1

    def step(message: str):
        nonlocal step_no
        print(f"\n[{step_no}] {message}")
        step_no += 1

    # ---- 1. Detect environment and select target --------------------------------
    env = detect_environment()
    print(f"\n[Environment] Detected: {env}")

    target: Optional[DeploymentTarget] = None

    if args.pod_name:
        target = K8sTarget(args.pod_name, namespace)
        print(f"    Using k8s pod: {target.name}")
    elif args.container_name:
        target = DockerTarget(args.container_name)
        print(f"    Using docker container: {target.name}")
    else:
        if env == "k8s":
            step(f"Finding ragflow pods in namespace '{namespace}' ...")
            pods = find_k8s_pods(namespace)
            if pods:
                target = pick_target(pods, "k8s")
                print(f"    Selected: {target.name}")
        elif env == "docker":
            step("Finding docker-ragflow containers ...")
            containers = find_docker_containers()
            if containers:
                target = pick_target(containers, "docker")
                print(f"    Selected: {target.name}")
        else:
            print("  [ERROR] No k8s pods or docker containers found")
            print("  Please ensure kubectl can reach a cluster or docker is running with ragflow containers")
            sys.exit(1)

    if target is None:
        print("  [ERROR] No suitable target found")
        sys.exit(1)

    if use_direct_rootfs and target.kind != "docker":
        print("  [ERROR] --direct-docker-rootfs is only supported for Docker targets")
        sys.exit(1)

    direct_rootfs: Optional[Path] = None
    if use_direct_rootfs:
        direct_rootfs = get_docker_rootfs(target)
        print(f"    Direct Docker rootfs sync enabled: {direct_rootfs / 'ragflow'}")

    # ---- 2. Setup rsyncd and port forward ------------------------------------
    rsync_ready = False

    def ensure_rsync_ready():
        nonlocal rsync_ready
        if rsync_ready:
            return
        step(f"Starting rsyncd in {target.kind} {target.name} ...")
        start_rsync_daemon(target, remote_port)
        if target.kind == "k8s":
            step("Setting up kubectl port-forward ...")
            start_k8s_port_forward(target, local_port, remote_port)
            if not wait_for_port(local_port, timeout=10):
                print(f"  [ERROR] rsync endpoint localhost:{local_port} not reachable")
                sys.exit(1)
            print(f"  rsync endpoint ready at rsync://localhost:{local_port}/ragflow")
        else:
            container_ip = get_docker_container_ip(target)
            print(f"  rsync endpoint ready at rsync://{container_ip}:{remote_port}/ragflow")
        rsync_ready = True

    try:
        # ---- 3. Frontend build + dist sync + nginx reload --------------------
        if needs_dist_sync:
            step("Building frontend (npm run build) ...")
            if args.build:
                if not run_npm_build():
                    print("  [ERROR] Frontend build failed. Aborting sync.")
                    sys.exit(1)
            if needs_rsync:
                ensure_rsync_ready()
            step("Syncing web/dist/ -> /ragflow/web/dist/ ...")
            if use_direct_rootfs:
                assert direct_rootfs is not None
                dist_synced = sync_dist_files_direct(direct_rootfs)
            else:
                dist_synced = sync_dist_files(target, local_port)
            if not dist_synced:
                sys.exit(1)
            step("Reloading nginx ...")
            reload_nginx(target)

        # ---- 4. Python sync + kill processes / restart container -------------
        if needs_python_sync:
            if needs_rsync:
                ensure_rsync_ready()
            step("Syncing Python files -> /ragflow/ ...")
            if use_direct_rootfs:
                assert direct_rootfs is not None
                python_synced = sync_python_files_direct(direct_rootfs)
            else:
                python_synced = sync_python_files(target, local_port)
            if not python_synced:
                sys.exit(1)
            step("Restarting RagFlow processes ...")
            if target.kind == "k8s":
                kill_ragflow_processes(target)
            else:
                # For docker, we can either kill processes or restart container
                # Restarting container is cleaner for major changes
                restart_container(target)

        print("\n✓ Sync complete!\n")

    finally:
        stop_port_forward()
        print("Done.")


if __name__ == "__main__":
    main()
