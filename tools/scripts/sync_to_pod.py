#!/usr/bin/env python3
"""Sync code changes to a running k8s pod without rebuilding image.

Usage:
    python sync_to_pod.py                  # Interactive: list pods, choose, sync
    python sync_to_pod.py --pod POD_NAME   # Direct sync to specific pod
    python sync_to_pod.py --python-only    # Skip npm build + dist sync
    python sync_to_pod.py --dist-only      # Only sync dist (after manual npm build)

Prereq:
    - rsync installed in the pod (apt install rsync)
    - kubectl configured with access to the cluster
    - Frontend: npm run build must work locally (Node.js required)
"""

import os
import subprocess
import time
import sys
import argparse
import socket
import shlex
from pathlib import Path


# ---------------------------------------------------------------------------
# kubectl / pod helpers
# ---------------------------------------------------------------------------

def find_ragflow_pods(namespace="ragflow"):
    """Return list of (pod_name, phase, age, node) for ragflow pods."""
    result = subprocess.run(
        ["kubectl", "get", "pods", "-n", namespace, "-l", "app=ragflow",
         "-o", "jsonpath={.items[*].metadata.name}"],
        capture_output=True, text=True
    )
    names = result.stdout.strip().split()
    if not names:
        raise RuntimeError(f"No ragflow pods found in namespace '{namespace}'")

    pods = []
    for name in names:
        # Get phase
        phase_r = subprocess.run(
            ["kubectl", "get", "pod", name, "-n", namespace,
             "-o", "jsonpath={.status.phase}"],
            capture_output=True, text=True
        )
        phase = phase_r.stdout.strip()
        # Get age
        age_r = subprocess.run(
            ["kubectl", "get", "pod", name, "-n", namespace,
             "-o", "jsonpath={.metadata.creationTimestamp}"],
            capture_output=True, text=True
        )
        age = age_r.stdout.strip()
        pods.append((name, phase, age))
    return pods


def pick_pod(pods):
    """Interactively pick a pod, or auto-pick the first Running one."""
    running = [p for p in pods if p[1] == "Running"]
    if not running:
        print("No running pods found. Aborting.")
        sys.exit(1)
    if len(running) == 1:
        return running[0][0]

    print("\nAvailable ragflow pods:")
    for i, (name, phase, age) in enumerate(pods, 1):
        marker = " (Running)" if phase == "Running" else f" ({phase})"
        print(f"  {i}. {name}{marker}")
    print()

    while True:
        try:
            choice = input(f"Select pod [1-{len(pods)}] or press Enter for default (1): ").strip()
            if choice == "":
                return running[0][0]
            idx = int(choice) - 1
            if 0 <= idx < len(pods):
                return pods[idx][0]
        except (ValueError, EOFError):
            pass
        print("Invalid selection.")


# ---------------------------------------------------------------------------
# rsyncd setup
# ---------------------------------------------------------------------------

def ensure_rsyncd_conf(pod, namespace="ragflow"):
    """Ensure /etc/rsyncd.conf exists in pod with a [ragflow] module."""
    content = """uid = root
gid = root
port = 8873
[ragflow]
    path = /ragflow
    read only = no
    list = yes
"""
    # Write conf file via stdin
    proc = subprocess.Popen(
        ["kubectl", "exec", "-i", pod, "-n", namespace, "--",
         "sh", "-c", "cat > /etc/rsyncd.conf"],
        stdin=subprocess.PIPE
    )
    proc.communicate(input=content.encode())
    if proc.returncode != 0:
        raise RuntimeError("Failed to write rsyncd.conf in pod")


def start_rsync_daemon(pod, namespace="ragflow"):
    """Kill any existing rsyncd, write config, start fresh daemon."""
    # Kill existing rsyncd
    subprocess.run(
        ["kubectl", "exec", pod, "-n", namespace, "--",
         "sh", "-c", "pkill -9 rsync 2>/dev/null; true"],
    )
    time.sleep(0.5)

    ensure_rsyncd_conf(pod, namespace)

    # Start rsyncd with nohup + log redirect
    subprocess.Popen(
        ["kubectl", "exec", pod, "-n", namespace, "--",
         "sh", "-c",
         "nohup rsync --daemon --no-detach --port=8873 "
         "> /tmp/rsyncd.log 2>&1 &"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
    )
    time.sleep(2)

    # Verify
    result = subprocess.run(
        ["kubectl", "exec", pod, "-n", namespace, "--",
         "sh", "-c", "ps aux | grep 'rsync --daemon' | grep -v grep"],
        capture_output=True, text=True
    )
    if not result.stdout.strip():
        # Try to show log for debugging
        log = subprocess.run(
            ["kubectl", "exec", pod, "-n", namespace, "--",
             "sh", "-c", "cat /tmp/rsyncd.log 2>/dev/null || echo '(no log)'"],
            capture_output=True, text=True
        ).stdout.strip()
        raise RuntimeError(f"rsyncd failed to start. Log: {log}")
    print(f"  rsyncd running in pod {pod}")


# ---------------------------------------------------------------------------
# port-forward
# ---------------------------------------------------------------------------

_port_forward_proc = None


def start_port_forward(pod, namespace="ragflow", local_port=8873, remote_port=8873):
    """Start kubectl port-forward in background. Returns process object."""
    global _port_forward_proc

    # Kill any old port-forward on that port
    subprocess.run(["pkill", "-f",
                    f"kubectl.*port-forward.*{local_port}:{remote_port}"],
                   capture_output=True)
    time.sleep(0.5)

    pf_proc = subprocess.Popen(
        ["kubectl", "port-forward", "-n", namespace, pod,
         f"{local_port}:{remote_port}"],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
    )
    _port_forward_proc = pf_proc
    time.sleep(3)

    if pf_proc.poll() is not None:
        raise RuntimeError("port-forward failed to start")

    # Verify port is listening
    def port_open(host, port, timeout=3):
        try:
            with socket.create_connection((host, port), timeout=timeout):
                return True
        except (socket.timeout, ConnectionRefusedError, OSError):
            return False

    max_retries = 10
    for attempt in range(max_retries):
        if port_open("localhost", local_port):
            break
        time.sleep(1)
    else:
        raise RuntimeError(
            f"port-forward started but localhost:{local_port} is not open "
            f"after {max_retries}s"
        )
    print(f"  port-forward active: localhost:{local_port} -> {pod}:{remote_port}")
    return pf_proc


def is_port_open(port, timeout=2):
    try:
        with socket.create_connection(("localhost", port), timeout=timeout):
            return True
    except (socket.timeout, ConnectionRefusedError, OSError):
        return False


# Repository root (two levels up from tools/scripts/)
_REPO_ROOT = Path(__file__).resolve().parent.parent.parent

# ---------------------------------------------------------------------------
# Build and sync
# ---------------------------------------------------------------------------

def run_npm_build():
    """Run npm install && npm run build in the web/ directory."""
    web_dir = _REPO_ROOT / "web"
    if not web_dir.exists():
        print("  [WARN] web/ directory not found, skipping frontend build")
        return False

    print("  Running npm install ...")
    result = subprocess.run(
        ["npm", "install"],
        cwd=web_dir,
        capture_output=True, text=True,
    )
    if result.returncode != 0:
        print(f"  [ERROR] npm install failed:\n{result.stderr[-500:]}")
        return False

    print("  Running npm run build ...")
    result = subprocess.run(
        ["npm", "run", "build"],
        cwd=web_dir,
        capture_output=True, text=True,
        env={**os.environ, "NODE_OPTIONS": "--max-old-space-size=8192"}
    )
    if result.returncode != 0:
        print(f"  [ERROR] npm run build failed:\n{result.stderr[-500:]}")
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


def sync_python_files(pod, namespace="ragflow", local_port=8873):
    """rsync all *.py files from repo root to pod's /ragflow."""
    repo_root = _REPO_ROOT
    dest = f"rsync://localhost:{local_port}/ragflow"

    print("  Syncing Python files to pod ...")
    cmd = [
        "rsync", "-avz", "--progress",
        "--rsync-path=rsync --daemon --no-detach --port=8873",
        *RSYNC_EXCLUDES,
        "--include=*/",
        "--include=**/*.py",
        "--exclude=*",
        str(repo_root) + "/",
        dest + "/",
    ]
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode != 0:
        print(f"  [ERROR] rsync failed:\n{result.stderr[-500:]}")
        return False
    # Print last 5 lines of output for summary
    lines = result.stdout.strip().split("\n")
    for line in lines[-5:]:
        print(f"    {line}")
    return True


def sync_dist_files(pod, namespace="ragflow", local_port=8873):
    """rsync web/dist/ to pod's /ragflow/web/dist/."""
    repo_root = _REPO_ROOT
    dist_dir = repo_root / "web" / "dist"

    if not dist_dir.exists():
        print("  [SKIP] web/dist/ does not exist (run with --all to include build)")
        return True  # Not an error

    dest = f"rsync://localhost:{local_port}/ragflow/web/dist"
    print("  Syncing web/dist/ to pod ...")

    cmd = [
        "rsync", "-avz", "--progress",
        "--rsync-path=rsync --daemon --no-detach --port=8873",
        str(dist_dir) + "/",
        dest + "/",
    ]
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode != 0:
        print(f"  [ERROR] rsync failed:\n{result.stderr[-500:]}")
        return False
    lines = result.stdout.strip().split("\n")
    for line in lines[-5:]:
        print(f"    {line}")
    return True


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

ENTRYPOINT_PYTHON_SCRIPTS = [
    "api/ragflow_server.py",
    "admin/server/admin_server.py",
    "rag/svr/sync_data_source.py",
]


def kill_ragflow_server(pod, namespace="ragflow"):
    """Kill entrypoint-managed Python processes in the pod to trigger restart."""
    ps_command = ["kubectl", "exec", pod, "-n", namespace, "--", "bash", "-lc", "ps -auxf"]
    print(f"  process snapshot before kill: {shlex.join(ps_command)}")
    ps_before = subprocess.run(ps_command, capture_output=True, text=True)
    if ps_before.returncode == 0:
        print(ps_before.stdout.rstrip())
    else:
        print(f"  [WARN] Failed to capture process snapshot before kill: {ps_before.stderr[-300:]}")

    list_command = f"pgrep -f {shlex.quote('|'.join(ENTRYPOINT_PYTHON_SCRIPTS))}"
    list_process_command = ["kubectl", "exec", pod, "-n", namespace, "--", "bash", "-lc", list_command]
    print(f"  process match command: {shlex.join(list_process_command)}")
    list_result = subprocess.run(
        list_process_command,
        capture_output=True,
        text=True,
    )
    if list_result.returncode not in (0, 1):
        print(f"  [WARN] Failed to list entrypoint-managed Python processes: {list_result.stderr[-300:]}")
        return

    processes = []
    for line in list_result.stdout.strip().splitlines():
        pid = line.strip()
        if not pid:
            continue
        describe_command = [
            "kubectl", "exec", pod, "-n", namespace, "--",
            "bash", "-lc", f"ps -p {shlex.quote(pid)} -o args=",
        ]
        describe_result = subprocess.run(describe_command, capture_output=True, text=True)
        command = describe_result.stdout.strip() if describe_result.returncode == 0 else ""
        processes.append((pid, command))

    if not processes:
        print("  no entrypoint-managed Python processes found")
        return

    for pid, command in processes:
        kill_command = [
            "kubectl", "exec", pod, "-n", namespace, "--",
            "bash", "-lc", f"kill -TERM {shlex.quote(pid)}",
        ]
        print(f"  killing pid={pid}: {command}")
        print(f"  command: {shlex.join(kill_command)}")
        result = subprocess.run(kill_command, capture_output=True, text=True)
        # exit code 143 = SIGTERM (container exec connection killed, likely means process died)
        # exit code 137 = SIGKILL. Both usually mean the kill succeeded but connection dropped.
        if result.returncode not in (0, 143, 137):
            print(f"  [WARN] Failed to signal pid={pid} command={command}: {result.stderr[-300:]}")
        else:
            print(f"  pid={pid} command={command} signaled for restart")

    print(f"  process snapshot after kill: {shlex.join(ps_command)}")
    ps_after = subprocess.run(ps_command, capture_output=True, text=True)
    if ps_after.returncode == 0:
        print(ps_after.stdout.rstrip())
    else:
        print(f"  [WARN] Failed to capture process snapshot after kill: {ps_after.stderr[-300:]}")


def reload_nginx(pod, namespace="ragflow"):
    """Send SIGHUP to nginx master to reload static assets after dist sync."""
    result = subprocess.run(
        ["kubectl", "exec", pod, "-n", namespace, "--",
         "sh", "-c",
         "pkill -HUP -f 'nginx: master' || true"],
        capture_output=True, text=True
    )
    if result.returncode in (0, 129, 143, 137):
        print("  nginx reloaded (SIGHUP sent)")
    else:
        print(f"  [WARN] Failed to reload nginx: {result.stderr[-200:]}")


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser(
        description="Sync code changes to a running ragflow pod."
    )
    parser.add_argument("-n", "--namespace", default="ragflow")
    parser.add_argument("-p", "--pod", dest="pod_name",
                        help="Specific pod name (skips selection prompt)")
    parser.add_argument("-l", "--local-port", type=int, default=8873)
    parser.add_argument("-r", "--remote-port", type=int, default=8873)
    parser.add_argument("--python-only", action="store_true",
                        help="Only sync Python files, skip npm build and dist")
    parser.add_argument("--dist-only", action="store_true",
                        help="Only sync web/dist (assumes build already done)")
    parser.add_argument("--build", action="store_true", default=True,
                        help="Run npm build before syncing (default: True)")
    parser.add_argument("--no-build", dest="build", action="store_false",
                        help="Skip npm build")
    args = parser.parse_args()

    namespace = args.namespace
    local_port = args.local_port
    remote_port = args.remote_port
    needs_dist_sync = not args.python_only
    needs_python_sync = not args.dist_only
    needs_rsync = needs_dist_sync or needs_python_sync

    # ---- 1. List and pick pod ------------------------------------------------
    print(f"\n[1] Finding ragflow pods in namespace '{namespace}' ...")
    pods = find_ragflow_pods(namespace)
    pod = args.pod_name or pick_pod(pods)
    print(f"    Selected: {pod}")

    pf_proc = None

    def ensure_rsync_ready():
        nonlocal pf_proc
        if pf_proc is not None:
            return
        print(f"\n[2] Starting rsyncd in pod {pod} ...")
        start_rsync_daemon(pod, namespace)
        print("\n[3] Setting up kubectl port-forward ...")
        pf_proc = start_port_forward(pod, namespace, local_port, remote_port)

        if not is_port_open(local_port):
            print(f"  [ERROR] rsync endpoint localhost:{local_port} not reachable")
            sys.exit(1)
        print("  rsync endpoint ready at rsync://localhost:{}/ragflow".format(local_port))

    try:
        # ---- 4. Frontend build + dist sync + nginx reload --------------------
        if needs_dist_sync:
            print("\n[4] Building frontend (npm run build) ...")
            if args.build:
                if not run_npm_build():
                    print("  [ERROR] Frontend build failed. Aborting sync to avoid deploying stale web/dist.")
                    sys.exit(1)
            if needs_rsync:
                ensure_rsync_ready()
            print("\n[5] Syncing web/dist/ -> /ragflow/web/dist/ ...")
            if not sync_dist_files(pod, namespace, local_port):
                sys.exit(1)
            print("\n[5b] Reloading nginx ...")
            reload_nginx(pod, namespace)

        # ---- 6. Python sync + kill ragflow_server ----------------------------
        if needs_python_sync:
            if needs_rsync:
                ensure_rsync_ready()
            print("\n[6] Syncing Python files -> /ragflow/ ...")
            if not sync_python_files(pod, namespace, local_port):
                sys.exit(1)
            print("\n[7] Killing ragflow_server process ...")
            kill_ragflow_server(pod, namespace)

        print("\n✓ Sync complete!\n")

    finally:
        if pf_proc is not None:
            print("Stopping port-forward ...")
            pf_proc.terminate()
            pf_proc.wait(timeout=5)
        print("Done.")


if __name__ == "__main__":
    main()
