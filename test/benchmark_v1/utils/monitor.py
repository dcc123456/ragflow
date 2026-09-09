"""System resource monitoring for benchmark tests."""

import subprocess
import threading
import time
from dataclasses import dataclass
from datetime import datetime
from typing import Callable, Optional

from .config_loader import config


@dataclass
class ResourceSnapshot:
    """A single snapshot of system resources."""
    timestamp: str
    container: str
    cpu_percent: float = 0.0
    memory_mb: float = 0.0
    network_rx_mb: float = 0.0
    network_tx_mb: float = 0.0
    disk_read_mb: float = 0.0
    disk_write_mb: float = 0.0


class ResourceMonitor:
    """Monitors system resources during benchmark tests."""

    def __init__(self, containers: Optional[list[str]] = None):
        self.enabled = config.monitoring_enabled
        self.interval = config.monitoring_interval
        self.containers = containers or config.get("monitoring.targets", [])
        self._stop_event = threading.Event()
        self._thread: Optional[threading.Thread] = None
        self.snapshots: list[ResourceSnapshot] = []

    def _get_container_stats(self, container: str) -> dict:
        """Get resource stats for a single container using docker stats."""
        try:
            result = subprocess.run(
                ["docker", "stats", container, "--no-stream", "--format",
                 "{{.CPUPerc}},{{.MemUsage}}"],
                capture_output=True, text=True, timeout=10
            )
            if result.returncode == 0:
                parts = result.stdout.strip().split(",")
                if len(parts) >= 2:
                    cpu = float(parts[0].replace("%", ""))
                    mem_str = parts[1].split("/")[0].strip()
                    mem_mb = self._parse_mem_to_mb(mem_str)
                    return {"cpu_percent": cpu, "memory_mb": mem_mb}
        except Exception:
            pass
        return {"cpu_percent": 0.0, "memory_mb": 0.0}

    def _parse_mem_to_mb(self, mem_str: str) -> float:
        """Parse memory string like '1.5GiB' or '512MiB' to MB."""
        mem_str = mem_str.strip().upper()
        if "GIB" in mem_str or "GB" in mem_str:
            return float(mem_str.replace("GIB", "").replace("GB", "")) * 1024
        elif "MIB" in mem_str or "MB" in mem_str:
            return float(mem_str.replace("MIB", "").replace("MB", ""))
        elif "KIB" in mem_str or "KB" in mem_str:
            return float(mem_str.replace("KIB", "").replace("KB", "")) / 1024
        return 0.0

    def _collect_snapshot(self) -> None:
        """Collect a snapshot of all containers."""
        for container in self.containers:
            stats = self._get_container_stats(container)
            self.snapshots.append(ResourceSnapshot(
                timestamp=datetime.now().isoformat(),
                container=container,
                **stats
            ))

    def _monitor_loop(self) -> None:
        """Background monitoring loop."""
        while not self._stop_event.is_set():
            self._collect_snapshot()
            self._stop_event.wait(self.interval)

    def start(self) -> None:
        """Start background monitoring."""
        if not self.enabled:
            return
        self._stop_event.clear()
        self._thread = threading.Thread(target=self._monitor_loop, daemon=True)
        self._thread.start()

    def stop(self) -> list[ResourceSnapshot]:
        """Stop monitoring and return collected snapshots."""
        self._stop_event.set()
        if self._thread:
            self._thread.join(timeout=5)
        return self.snapshots

    def get_summary(self) -> dict:
        """Get summary statistics from collected snapshots."""
        if not self.snapshots:
            return {}

        summary = {}
        for container in self.containers:
            container_snaps = [s for s in self.snapshots if s.container == container]
            if not container_snaps:
                continue
            cpus = [s.cpu_percent for s in container_snaps]
            mems = [s.memory_mb for s in container_snaps]
            summary[container] = {
                "cpu_avg": sum(cpus) / len(cpus),
                "cpu_max": max(cpus),
                "memory_avg_mb": sum(mems) / len(mems),
                "memory_max_mb": max(mems),
            }
        return summary
