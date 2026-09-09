"""Metrics collector for benchmark tests."""

import json
import time
from collections import defaultdict
from dataclasses import asdict, dataclass
from datetime import datetime
from pathlib import Path
from typing import Any

import numpy as np

from .config_loader import config


@dataclass
class LatencyPercentiles:
    """Latency percentile metrics."""
    p50: float = 0.0
    p90: float = 0.0
    p99: float = 0.0


@dataclass
class IngestionMetrics:
    """Metrics for ingestion tests."""
    total_requests: int = 0
    successful_requests: int = 0
    failed_requests: int = 0
    total_docs: int = 0
    successful_docs: int = 0
    failed_docs: int = 0
    total_bytes: int = 0
    total_time_ms: float = 0.0
    avg_latency_ms: float = 0.0
    throughput_per_sec: float = 0.0
    success_rate: float = 0.0
    stage_avg: dict = None

    def __post_init__(self):
        if self.stage_avg is None:
            self.stage_avg = {"upload": 0.0, "parse": 0.0, "embedding": 0.0, "indexing": 0.0}


@dataclass
class RetrievalMetrics:
    """Metrics for retrieval tests."""
    total_requests: int = 0
    successful_requests: int = 0
    failed_requests: int = 0
    total_queries: int = 0
    latency_ms: list = None
    latency_percentiles: LatencyPercentiles = None
    qps: float = 0.0
    success_rate: float = 0.0
    duration_sec: float = 0.0

    def __post_init__(self):
        if self.latency_ms is None:
            self.latency_ms = []
        if self.latency_percentiles is None:
            self.latency_percentiles = LatencyPercentiles()


class MetricsCollector:
    """Collects and aggregates benchmark metrics."""

    def __init__(self, test_name: str):
        self.test_name = test_name
        self.start_time = time.time()
        self.results_dir = Path(config.results_dir) / test_name
        self.results_dir.mkdir(parents=True, exist_ok=True)

        # Raw data storage
        self.ingestion_raw: list[dict] = []
        self.retrieval_raw: list[dict] = []
        self.monitoring_raw: list[dict] = []

    def record_ingestion(self, doc_path: str, result: Any, elapsed_ms: float, stages: dict) -> None:
        """Record a single ingestion result."""
        self.ingestion_raw.append({
            "timestamp": datetime.now().isoformat(),
            "doc_path": doc_path,
            "success": result.success if hasattr(result, "success") else False,
            "elapsed_ms": elapsed_ms,
            "stages": stages,
        })

    def record_retrieval(self, query_id: int, latency_ms: float, success: bool) -> None:
        """Record a single retrieval result."""
        self.retrieval_raw.append({
            "timestamp": datetime.now().isoformat(),
            "query_id": query_id,
            "latency_ms": latency_ms,
            "success": success,
        })

    def compute_ingestion_metrics(self) -> IngestionMetrics:
        """Compute aggregated ingestion metrics."""
        if not self.ingestion_raw:
            return IngestionMetrics()

        metrics = IngestionMetrics()
        metrics.total_docs = len(self.ingestion_raw)
        successful = [r for r in self.ingestion_raw if r["success"]]
        metrics.successful_docs = len(successful)
        metrics.failed_docs = metrics.total_docs - metrics.successful_docs
        metrics.success_rate = metrics.successful_docs / metrics.total_docs if metrics.total_docs > 0 else 0

        total_time = sum(r["elapsed_ms"] for r in self.ingestion_raw)
        metrics.total_time_ms = total_time
        metrics.avg_latency_ms = total_time / metrics.total_docs if metrics.total_docs > 0 else 0

        duration = time.time() - self.start_time
        metrics.throughput_per_sec = metrics.successful_docs / duration if duration > 0 else 0

        # Stage averages
        stage_keys = ["upload", "parse", "embedding", "indexing"]
        for sk in stage_keys:
            vals = [r["stages"].get(sk, 0) for r in successful if sk in r.get("stages", {})]
            metrics.stage_avg[sk] = sum(vals) / len(vals) if vals else 0

        return metrics

    def compute_retrieval_metrics(self) -> RetrievalMetrics:
        """Compute aggregated retrieval metrics."""
        if not self.retrieval_raw:
            return RetrievalMetrics()

        metrics = RetrievalMetrics()
        metrics.total_queries = len(self.retrieval_raw)
        successful = [r for r in self.retrieval_raw if r["success"]]
        metrics.successful_requests = len(successful)
        metrics.failed_requests = metrics.total_queries - metrics.successful_requests
        metrics.success_rate = metrics.successful_requests / metrics.total_queries if metrics.total_queries > 0 else 0

        latencies = [r["latency_ms"] for r in self.retrieval_raw]
        if latencies:
            metrics.latency_ms = latencies
            metrics.latency_percentiles.p50 = float(np.percentile(latencies, 50))
            metrics.latency_percentiles.p90 = float(np.percentile(latencies, 90))
            metrics.latency_percentiles.p99 = float(np.percentile(latencies, 99))

        duration = time.time() - self.start_time
        metrics.duration_sec = duration
        metrics.qps = metrics.successful_requests / duration if duration > 0 else 0

        return metrics

    def save_results(self) -> str:
        """Save all collected metrics to files."""
        timestamp = datetime.now().strftime("%Y%m%d_%H%M%S")
        prefix = f"{self.test_name}_{timestamp}"

        # Save raw data
        raw_path = self.results_dir / f"{prefix}_raw.json"
        with open(raw_path, "w", encoding="utf-8") as f:
            json.dump({
                "ingestion": self.ingestion_raw,
                "retrieval": self.retrieval_raw,
                "monitoring": self.monitoring_raw,
            }, f, indent=2, ensure_ascii=False)

        # Save aggregated metrics
        metrics_path = self.results_dir / f"{prefix}_metrics.json"
        with open(metrics_path, "w", encoding="utf-8") as f:
            json.dump({
                "ingestion": asdict(self.compute_ingestion_metrics()),
                "retrieval": asdict(self.compute_retrieval_metrics()),
            }, f, indent=2, ensure_ascii=False)

        return str(metrics_path)
