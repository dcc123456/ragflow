#!/usr/bin/env python3
"""
RAGFlow 纯文字检索压测脚本

测试目标：检索接口的吞吐量（QPS）和延迟（P50/P99）成功率等

Usage:
    # 基准测试：1并发 × 持续 5/10 分钟
    python scripts/retrieval_test.py --kb-id <kb_id> --test-type baseline --duration 5 --concurrent 1

    # 梯度测试：1：5：10：15：20 并发
    python scripts/retrieval_test.py --kb-id <kb_id> --test-type gradient --concurrent-levels 1,5,10,15,20

    # 稳定测试：某并发下长时间运行
    python scripts/retrieval_test.py --kb-id <kb_id> --test-type stability --concurrent 10 --duration 20

kb_id 只能通过 --kb-id 或配置文件（knowledge_base.kb_id）指定，脚本不会创建知识库。
"""

import argparse
import json
import sys
import threading
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Optional

sys.path.insert(0, str(Path(__file__).parent.parent))

from utils import config, RAGFlowApiClient, MetricsCollector, LatencyPercentiles


@dataclass
class RetrievalTestConfig:
    """Retrieval test configuration."""
    kb_id: str
    queries: list[dict]
    concurrent: int
    duration_minutes: int
    qps_limit: Optional[float] = None  # Optional QPS limit per thread


class RetrievalBenchmark:
    """纯文字检索压测主类"""

    def __init__(self):
        self.client = RAGFlowApiClient()
        self.kb_id = config.kb_id
        self.queries: list[dict] = []

    def load_queries(self, query_file: Optional[str] = None) -> list[dict]:
        """加载查询语句"""
        if not self.queries:
            qfile = query_file or config.query_file
            if qfile and Path(qfile).exists():
                with open(qfile, "r", encoding="utf-8") as f:
                    data = json.load(f)
                    self.queries = data.get("queries", [])
            if not self.queries:
                # Fallback: generate dummy queries
                self.queries = [{"id": i, "text": f"query_{i}"} for i in range(100)]
        return self.queries

    def baseline_test(self, concurrent: int = 1, duration_minutes: int = 5) -> dict:
        """基准测试：1并发 × 持续 5/10 分钟"""
        print("=" * 60)
        print(f"Baseline Test - Concurrent: {concurrent}, Duration: {duration_minutes}min")
        print("=" * 60)

        queries = self.load_queries()
        collector = MetricsCollector("retrieval_baseline")
        monitor_enabled = True

        from utils import ResourceMonitor
        monitor = ResourceMonitor()
        monitor.start()

        start_time = time.time()
        duration_seconds = duration_minutes * 60
        query_index = 0
        active_workers = threading.active_count()
        lock = threading.Lock()

        def worker():
            nonlocal query_index
            while time.time() - start_time < duration_seconds:
                query = queries[query_index % len(queries)]
                query_index += 1

                resp = self.client.retrieval(self.kb_id, query["text"])
                latency = resp.elapsed_ms
                success = resp.success

                with lock:
                    collector.record_retrieval(query["id"], latency, success)

                # Small delay to avoid overwhelming
                if concurrent <= 5:
                    time.sleep(0.01)

        threads = []
        for _ in range(concurrent):
            t = threading.Thread(target=worker, daemon=True)
            t.start()
            threads.append(t)

        # Monitor progress
        last_count = 0
        while time.time() - start_time < duration_seconds:
            time.sleep(10)
            with lock:
                current = len(collector.retrieval_raw)
            elapsed = time.time() - start_time
            print(f"  Progress: {elapsed:.0f}s / {duration_seconds}s | Requests: {current} | Rate: {current / elapsed:.2f}/s")

        # Wait for threads to complete current batch
        time.sleep(1)

        snapshots = monitor.stop()
        metrics = collector.compute_retrieval_metrics()

        print("\n" + "=" * 60)
        print("Baseline Test Results")
        print("=" * 60)
        self._print_retrieval_metrics(metrics)

        results_path = collector.save_results()
        print(f"\nResults saved to: {results_path}")

        return metrics

    def gradient_test(self, concurrent_levels: list[int], duration_minutes: int = 3) -> list[dict]:
        """梯度测试：从单并发逐步增加到高并发"""
        print("=" * 60)
        print(f"Gradient Test - Concurrency levels: {concurrent_levels}, Duration per level: {duration_minutes}min")
        print("=" * 60)

        queries = self.load_queries()
        results = []

        from utils import ResourceMonitor

        for concurrent in concurrent_levels:
            print(f"\n{'=' * 40}")
            print(f"Testing with concurrency: {concurrent}")
            print(f"{'=' * 40}")

            collector = MetricsCollector(f"retrieval_gradient_c{concurrent}")
            monitor = ResourceMonitor()
            monitor.start()

            start_time = time.time()
            duration_seconds = duration_minutes * 60
            query_index = 0
            lock = threading.Lock()

            def worker():
                nonlocal query_index
                while time.time() - start_time < duration_seconds:
                    query = queries[query_index % len(queries)]
                    query_index += 1

                    resp = self.client.retrieval(self.kb_id, query["text"])
                    latency = resp.elapsed_ms
                    success = resp.success

                    with lock:
                        collector.record_retrieval(query["id"], latency, success)

            threads = []
            for _ in range(concurrent):
                t = threading.Thread(target=worker, daemon=True)
                t.start()
                threads.append(t)

            # Monitor progress
            while time.time() - start_time < duration_seconds:
                time.sleep(10)
                with lock:
                    current = len(collector.retrieval_raw)
                elapsed = time.time() - start_time
                print(f"  Progress: {elapsed:.0f}s / {duration_seconds}s | Requests: {current} | Rate: {current / elapsed:.2f}/s")

            time.sleep(1)

            snapshots = monitor.stop()
            metrics = collector.compute_retrieval_metrics()

            print(f"\nConcurrency {concurrent} Results:")
            self._print_retrieval_metrics(metrics)

            results.append({
                "concurrent": concurrent,
                "metrics": metrics,
            })

            time.sleep(5)  # Cool down between tests

        return results

    def stability_test(self, concurrent: int, duration_minutes: int) -> dict:
        """稳定测试：某并发下的长时间运行"""
        print("=" * 60)
        print(f"Stability Test - Concurrent: {concurrent}, Duration: {duration_minutes}min")
        print("=" * 60)

        queries = self.load_queries()
        collector = MetricsCollector("retrieval_stability")

        from utils import ResourceMonitor
        monitor = ResourceMonitor()
        monitor.start()

        start_time = time.time()
        duration_seconds = duration_minutes * 60
        query_index = 0
        lock = threading.Lock()

        def worker():
            nonlocal query_index
            while time.time() - start_time < duration_seconds:
                query = queries[query_index % len(queries)]
                query_index += 1

                resp = self.client.retrieval(self.kb_id, query["text"])
                latency = resp.elapsed_ms
                success = resp.success

                with lock:
                    collector.record_retrieval(query["id"], latency, success)

        threads = []
        for _ in range(concurrent):
            t = threading.Thread(target=worker, daemon=True)
            t.start()
            threads.append(t)

        # Monitor progress
        last_count = 0
        while time.time() - start_time < duration_seconds:
            time.sleep(30)
            with lock:
                current = len(collector.retrieval_raw)
            elapsed = time.time() - start_time
            print(f"  Progress: {elapsed / 60:.1f}min / {duration_minutes}min | Requests: {current} | Rate: {current / elapsed:.2f}/s")

        time.sleep(1)

        snapshots = monitor.stop()
        metrics = collector.compute_retrieval_metrics()

        print("\n" + "=" * 60)
        print("Stability Test Results")
        print("=" * 60)
        self._print_retrieval_metrics(metrics)

        results_path = collector.save_results()
        print(f"\nResults saved to: {results_path}")

        return metrics

    def _print_retrieval_metrics(self, metrics) -> None:
        """打印检索指标"""
        p = metrics.latency_percentiles
        print(f"  Total queries:    {metrics.total_queries}")
        print(f"  Successful:       {metrics.successful_requests}")
        print(f"  Failed:           {metrics.failed_requests}")
        print(f"  Success rate:     {metrics.success_rate * 100:.2f}%")
        print(f"  QPS:              {metrics.qps:.2f}")
        print(f"  Latency P50:      {p.p50:.2f}ms")
        print(f"  Latency P90:      {p.p90:.2f}ms")
        print(f"  Latency P99:      {p.p99:.2f}ms")


def main():
    parser = argparse.ArgumentParser(description="RAGFlow 纯文字检索压测")
    parser.add_argument("--test-type", choices=["baseline", "gradient", "stability"],
                        default="baseline", help="Type of test to run")
    parser.add_argument("--concurrent", type=int, default=1,
                        help="Number of concurrent requests")
    parser.add_argument("--concurrent-levels", type=str, default="1,2,4,8,16",
                        help="Comma-separated concurrency levels for gradient test")
    parser.add_argument("--duration", type=int, default=5,
                        help="Duration in minutes")
    parser.add_argument("--query-file", type=str, default=None,
                        help="Path to query JSON file")
    parser.add_argument("--kb-id", type=str, default=None,
                        help="Knowledge base ID to test against (required, overrides config)")
    args = parser.parse_args()

    benchmark = RetrievalBenchmark()

    # kb_id only comes from --kb-id or the config file; never create a new one.
    kb_id = args.kb_id or config.kb_id
    if not kb_id or kb_id in ("your-kb-id-here", "auto"):
        print("Error: no knowledge base id configured. "
              "Pass --kb-id <kb_id> or set knowledge_base.kb_id in the config file.")
        sys.exit(1)

    resp = benchmark.client.get_knowledge_base(kb_id)
    if not resp.success:
        print(f"Error: knowledge base {kb_id} is not accessible: {resp.error}")
        sys.exit(1)
    benchmark.kb_id = kb_id
    print(f"Using knowledge base: {kb_id}")

    # Check service health
    health = benchmark.client.health_check()
    if not health.success:
        print(f"Warning: RAGFlow service may not be healthy: {health.error}")

    if args.test_type == "baseline":
        benchmark.baseline_test(args.concurrent, args.duration)
    elif args.test_type == "gradient":
        levels = [int(x) for x in args.concurrent_levels.split(",")]
        benchmark.gradient_test(levels, args.duration)
    elif args.test_type == "stability":
        benchmark.stability_test(args.concurrent, args.duration)


if __name__ == "__main__":
    main()
