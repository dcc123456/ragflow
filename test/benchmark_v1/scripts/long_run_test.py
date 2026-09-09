#!/usr/bin/env python3
"""
RAGFlow 长稳测试脚本

测试目标：验证 RAGFlow 在长时间持续运行的稳定性、可靠性和性能一致性

Usage:
    # 纯检索长稳测试 4小时
    python scripts/long_run_test.py --kb-id <kb_id> --test-type retrieval --duration 240 --concurrent 10

    # 纯入库长稳测试 8小时
    python scripts/long_run_test.py --kb-id <kb_id> --test-type ingestion --duration 480 --concurrent 4

kb_id 只能通过 --kb-id 或配置文件（knowledge_base.kb_id）指定，脚本不会创建知识库。
"""

import argparse
import glob
import json
import os
import sys
import threading
import time
from pathlib import Path
from typing import Optional

sys.path.insert(0, str(Path(__file__).parent.parent))

from utils import config, RAGFlowApiClient, MetricsCollector, ResourceMonitor


class LongRunBenchmark:
    """长稳测试主类"""

    def __init__(self):
        self.client = RAGFlowApiClient()
        self.kb_id = config.kb_id
        self.queries: list[dict] = []
        self.files: list[str] = []

    def load_test_data(self, file_type: str = "chunks") -> None:
        """加载测试数据"""
        # Load queries
        query_file = config.query_file
        if query_file and Path(query_file).exists():
            with open(query_file, "r", encoding="utf-8") as f:
                data = json.load(f)
                self.queries = data.get("queries", [])
        if not self.queries:
            self.queries = [{"id": i, "text": f"query_{i}"} for i in range(100)]

        # Load files
        dir_map = {
            "small": config.small_pdfs_dir,
            "medium": config.medium_pdfs_dir,
            "large": config.large_pdfs_dir,
            "chunks": config.chunks_pdfs_dir,
        }
        target_dir = dir_map.get(file_type, config.chunks_pdfs_dir)
        pattern = os.path.join(target_dir, "*.pdf")
        self.files = sorted(glob.glob(pattern))
        if not self.files:
            print(f"Warning: No PDF files found in {pattern}")

    def run_retrieval_long_run(self, concurrent: int, duration_minutes: int) -> dict:
        """纯检索长稳测试"""
        print("=" * 60)
        print(f"Long Run Test - Retrieval Only")
        print(f"  Concurrent: {concurrent}")
        print(f"  Duration: {duration_minutes}min ({duration_minutes / 60:.1f} hours)")
        print("=" * 60)

        self.load_test_data()

        collector = MetricsCollector("long_run_retrieval")
        monitor = ResourceMonitor()
        monitor.start()

        start_time = time.time()
        duration_seconds = duration_minutes * 60
        query_index = 0
        lock = threading.Lock()

        # Track performance over time windows
        window_size = 300  # 5-minute windows
        windows = []

        def worker():
            nonlocal query_index
            window_start = time.time()
            window_count = 0

            while time.time() - start_time < duration_seconds:
                query = self.queries[query_index % len(self.queries)]
                query_index += 1

                resp = self.client.retrieval(self.kb_id, query["text"])
                latency = resp.elapsed_ms
                success = resp.success

                with lock:
                    collector.record_retrieval(query["id"], latency, success)
                    window_count += 1

                # Check if window completed
                if time.time() - window_start >= window_size:
                    with lock:
                        windows.append({
                            "start": window_start - start_time,
                            "count": window_count,
                            "elapsed": time.time() - window_start,
                        })
                    window_start = time.time()
                    window_count = 0

        threads = []
        for _ in range(concurrent):
            t = threading.Thread(target=worker, daemon=True)
            t.start()
            threads.append(t)

        # Monitor progress
        last_check = time.time()
        while time.time() - start_time < duration_seconds:
            time.sleep(60)
            elapsed = time.time() - start_time
            elapsed_min = elapsed / 60
            with lock:
                current = len(collector.retrieval_raw)
            rate = current / elapsed if elapsed > 0 else 0
            print(f"  [{elapsed_min:.0f}/{duration_minutes}min] Requests: {current} | Rate: {rate:.2f}/s | "
                  f"Progress: {elapsed / duration_seconds * 100:.1f}%")

            # Periodic metrics snapshot
            if time.time() - last_check >= 300:  # Every 5 minutes
                last_check = time.time()

        time.sleep(1)

        snapshots = monitor.stop()
        metrics = collector.compute_retrieval_metrics()
        resource_summary = monitor.get_summary()

        print("\n" + "=" * 60)
        print("Long Run Test Results - Retrieval")
        print("=" * 60)

        p = metrics.latency_percentiles
        print(f"  Total queries:    {metrics.total_queries}")
        print(f"  Successful:       {metrics.successful_requests}")
        print(f"  Failed:           {metrics.failed_requests}")
        print(f"  Success rate:     {metrics.success_rate * 100:.4f}%")
        print(f"  QPS:              {metrics.qps:.4f}")
        print(f"  Latency P50:      {p.p50:.2f}ms")
        print(f"  Latency P90:      {p.p90:.2f}ms")
        print(f"  Latency P99:      {p.p99:.2f}ms")

        print("\n  Resource Summary:")
        for container, stats in resource_summary.items():
            print(f"    {container}:")
            print(f"      CPU avg: {stats.get('cpu_avg', 0):.2f}% | max: {stats.get('cpu_max', 0):.2f}%")
            print(f"      Memory avg: {stats.get('memory_avg_mb', 0):.2f}MB | max: {stats.get('memory_max_mb', 0):.2f}MB")

        results_path = collector.save_results()
        print(f"\n  Results saved to: {results_path}")

        return {
            "metrics": metrics,
            "resource_summary": resource_summary,
        }

    def run_ingestion_long_run(self, concurrent: int, duration_minutes: int, file_type: str = "chunks") -> dict:
        """纯入库长稳测试"""
        print("=" * 60)
        print(f"Long Run Test - Ingestion Only")
        print(f"  Concurrent: {concurrent}")
        print(f"  Duration: {duration_minutes}min ({duration_minutes / 60:.1f} hours)")
        print("=" * 60)

        self.load_test_data(file_type)
        if not self.files:
            print("Error: No test files available")
            return {}

        collector = MetricsCollector("long_run_ingestion")
        monitor = ResourceMonitor()
        monitor.start()

        start_time = time.time()
        duration_seconds = duration_minutes * 60
        file_index = 0
        lock = threading.Lock()

        def worker():
            nonlocal file_index
            while time.time() - start_time < duration_seconds:
                file_path = self.files[file_index % len(self.files)]
                file_index += 1

                result = self.client.upload_document(self.kb_id, file_path)
                stages_dict = {
                    "upload": result.stages.upload if result.stages else 0,
                    "parse": result.stages.parse if result.stages else 0,
                    "chunk": result.stages.chunk if result.stages else 0,
                    "embedding": result.stages.embedding if result.stages else 0,
                    "indexing": result.stages.indexing if result.stages else 0,
                }

                with lock:
                    collector.record_ingestion(file_path, result, result.total_time_ms, stages_dict)

        threads = []
        for _ in range(concurrent):
            t = threading.Thread(target=worker, daemon=True)
            t.start()
            threads.append(t)

        # Monitor progress
        last_check = time.time()
        while time.time() - start_time < duration_seconds:
            time.sleep(60)
            elapsed = time.time() - start_time
            elapsed_min = elapsed / 60
            with lock:
                current = collector.ingestion_raw
                successful = len([r for r in current if r["success"]])
                failed = len([r for r in current if not r["success"]])
            docs_per_min = successful / elapsed_min if elapsed_min > 0 else 0
            print(f"  [{elapsed_min:.0f}/{duration_minutes}min] Docs: {successful + failed} | "
                  f"Success: {successful} | Failed: {failed} | Rate: {docs_per_min:.2f} docs/min | "
                  f"Progress: {elapsed / duration_seconds * 100:.1f}%")

        time.sleep(1)

        snapshots = monitor.stop()
        metrics = collector.compute_ingestion_metrics()
        resource_summary = monitor.get_summary()

        print("\n" + "=" * 60)
        print("Long Run Test Results - Ingestion")
        print("=" * 60)

        print(f"  Total docs:       {metrics.total_docs}")
        print(f"  Successful:       {metrics.successful_docs}")
        print(f"  Failed:           {metrics.failed_docs}")
        print(f"  Success rate:     {metrics.success_rate * 100:.4f}%")
        print(f"  Throughput:       {metrics.throughput_per_sec * 60:.4f} docs/min")

        print("\n  Resource Summary:")
        for container, stats in resource_summary.items():
            print(f"    {container}:")
            print(f"      CPU avg: {stats.get('cpu_avg', 0):.2f}% | max: {stats.get('cpu_max', 0):.2f}%")
            print(f"      Memory avg: {stats.get('memory_avg_mb', 0):.2f}MB | max: {stats.get('memory_max_mb', 0):.2f}MB")

        results_path = collector.save_results()
        print(f"\n  Results saved to: {results_path}")

        return {
            "metrics": metrics,
            "resource_summary": resource_summary,
        }


def main():
    parser = argparse.ArgumentParser(description="RAGFlow 长稳测试")
    parser.add_argument("--test-type", choices=["retrieval", "ingestion"],
                        default="retrieval", help="Type of long run test")
    parser.add_argument("--concurrent", type=int, default=None,
                        help="Concurrent threads (default: 10 for retrieval, 4 for ingestion)")
    parser.add_argument("--duration", type=int, default=240,
                        help="Duration in minutes")
    parser.add_argument("--file-type", choices=["small", "medium", "large", "chunks"],
                        default="chunks", help="Type of files for ingestion test")
    parser.add_argument("--kb-id", type=str, default=None,
                        help="Knowledge base ID to test against (required, overrides config)")
    args = parser.parse_args()

    benchmark = LongRunBenchmark()

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

    # Both test types share --concurrent; the default depends on the test type.
    concurrent = args.concurrent if args.concurrent is not None else (
        10 if args.test_type == "retrieval" else 4
    )

    if args.test_type == "retrieval":
        benchmark.run_retrieval_long_run(concurrent, args.duration)
    else:
        benchmark.run_ingestion_long_run(concurrent, args.duration, args.file_type)


if __name__ == "__main__":
    main()
