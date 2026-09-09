#!/usr/bin/env python3
"""
RAGFlow 混合负载测试脚本

测试目标：检索（读）与入库（写）同时进行的混合负载场景下的系统表现

Usage:
    # 运行混合负载测试
    python scripts/mixed_workload_test.py --kb-id <kb_id> --retrieval-concurrent 10 --ingestion-concurrent 4

    # 读写比例测试矩阵
    python scripts/mixed_workload_test.py --kb-id <kb_id> --test-matrix

kb_id 只能通过 --kb-id 或配置文件（knowledge_base.kb_id）指定，脚本不会创建知识库。
"""

import argparse
import glob
import json
import os
import sys
import threading
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional

sys.path.insert(0, str(Path(__file__).parent.parent))

from utils import config, RAGFlowApiClient, MetricsCollector, ResourceMonitor


@dataclass
class MixedWorkloadConfig:
    """Mixed workload test configuration."""
    retrieval_concurrent: int
    ingestion_concurrent: int
    duration_minutes: int
    file_type: str = "chunks"


class MixedWorkloadBenchmark:
    """混合负载测试主类"""

    # Test matrix from requirements
    TEST_MATRIX = [
        {"name": "A", "retrieval": 1, "ingestion": 1},
        {"name": "B", "retrieval": 5, "ingestion": 2},
        {"name": "C", "retrieval": 10, "ingestion": 4},
        {"name": "D", "retrieval": 5, "ingestion": 8},
        {"name": "E", "retrieval": 10, "ingestion": 8},
    ]

    def __init__(self):
        self.client = RAGFlowApiClient()
        self.kb_id = config.kb_id
        self.queries: list[dict] = []
        self.files: list[str] = []

    def load_test_data(self, file_type: str = "small") -> None:
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

    def run_mixed_test(self, retrieval_concurrent: int, ingestion_concurrent: int,
                       duration_minutes: int, file_type: str = "chunks") -> dict:
        """运行混合负载测试"""
        print("=" * 60)
        print(f"Mixed Workload Test")
        print(f"  Retrieval concurrent: {retrieval_concurrent}")
        print(f"  Ingestion concurrent: {ingestion_concurrent}")
        print(f"  Duration: {duration_minutes}min")
        print("=" * 60)

        self.load_test_data(file_type)

        collector = MetricsCollector(f"mixed_r{retrieval_concurrent}_i{ingestion_concurrent}")
        monitor = ResourceMonitor()
        monitor.start()

        start_time = time.time()
        duration_seconds = duration_minutes * 60
        query_index = 0
        file_index = 0
        lock = threading.Lock()

        retrieval_count = 0
        ingestion_count = 0
        retrieval_errors = 0
        ingestion_errors = 0

        def retrieval_worker():
            nonlocal query_index, retrieval_count, retrieval_errors
            while time.time() - start_time < duration_seconds:
                query = self.queries[query_index % len(self.queries)]
                query_index += 1

                resp = self.client.retrieval(self.kb_id, query["text"])
                with lock:
                    retrieval_count += 1
                    if not resp.success:
                        retrieval_errors += 1
                    collector.record_retrieval(query["id"], resp.elapsed_ms, resp.success)

        def ingestion_worker():
            nonlocal file_index, ingestion_count, ingestion_errors
            while time.time() - start_time < duration_seconds:
                if not self.files:
                    continue
                file_path = self.files[file_index % len(self.files)]
                file_index += 1

                result = self.client.upload_document(self.kb_id, file_path)
                with lock:
                    ingestion_count += 1
                    if not result.success:
                        ingestion_errors += 1
                    stages_dict = {
                        "upload": result.stages.upload if result.stages else 0,
                        "parse": result.stages.parse if result.stages else 0,
                        "chunk": result.stages.chunk if result.stages else 0,
                        "embedding": result.stages.embedding if result.stages else 0,
                        "indexing": result.stages.indexing if result.stages else 0,
                    }
                    collector.record_ingestion(file_path, result, result.total_time_ms, stages_dict)

        threads = []
        for _ in range(retrieval_concurrent):
            t = threading.Thread(target=retrieval_worker, daemon=True)
            t.start()
            threads.append(t)
        for _ in range(ingestion_concurrent):
            t = threading.Thread(target=ingestion_worker, daemon=True)
            t.start()
            threads.append(t)

        # Monitor progress
        while time.time() - start_time < duration_seconds:
            time.sleep(30)
            elapsed = time.time() - start_time
            with lock:
                r_count = retrieval_count
                i_count = ingestion_count
            print(f"  Progress: {elapsed / 60:.1f}min / {duration_minutes}min | "
                  f"Retrieval: {r_count} ({r_count / elapsed:.2f}/s) | "
                  f"Ingestion: {i_count} ({i_count / elapsed:.2f}/min)")

        time.sleep(1)

        snapshots = monitor.stop()
        ingestion_metrics = collector.compute_ingestion_metrics()
        retrieval_metrics = collector.compute_retrieval_metrics()

        print("\n" + "=" * 60)
        print("Mixed Workload Test Results")
        print("=" * 60)
        print("\nRetrieval Metrics:")
        p = retrieval_metrics.latency_percentiles
        print(f"  Total queries:    {retrieval_metrics.total_queries}")
        print(f"  Successful:       {retrieval_metrics.successful_requests}")
        print(f"  Failed:           {retrieval_metrics.failed_requests}")
        print(f"  Success rate:     {retrieval_metrics.success_rate * 100:.2f}%")
        print(f"  QPS:              {retrieval_metrics.qps:.2f}")
        print(f"  Latency P50:      {p.p50:.2f}ms")
        print(f"  Latency P90:      {p.p90:.2f}ms")
        print(f"  Latency P99:      {p.p99:.2f}ms")

        print("\nIngestion Metrics:")
        print(f"  Total docs:       {ingestion_metrics.total_docs}")
        print(f"  Successful:       {ingestion_metrics.successful_docs}")
        print(f"  Failed:           {ingestion_metrics.failed_docs}")
        print(f"  Success rate:     {ingestion_metrics.success_rate * 100:.2f}%")
        print(f"  Throughput:       {ingestion_metrics.throughput_per_sec * 60:.2f} docs/min")

        results_path = collector.save_results()
        print(f"\nResults saved to: {results_path}")

        return {
            "retrieval": retrieval_metrics,
            "ingestion": ingestion_metrics,
        }

    def run_test_matrix(self, duration_minutes: int = 3, file_type: str = "chunks") -> list[dict]:
        """运行测试矩阵"""
        print("=" * 60)
        print("Running Test Matrix")
        print("=" * 60)

        self.load_test_data(file_type)
        results = []

        for test_case in self.TEST_MATRIX:
            name = test_case["name"]
            r_conc = test_case["retrieval"]
            i_conc = test_case["ingestion"]

            print(f"\n{'=' * 40}")
            print(f"Test Case {name}: Retrieval={r_conc}, Ingestion={i_conc}")
            print(f"{'=' * 40}")

            try:
                result = self.run_mixed_test(r_conc, i_conc, duration_minutes, file_type)
                results.append({
                    "name": name,
                    "retrieval_concurrent": r_conc,
                    "ingestion_concurrent": i_conc,
                    "result": result,
                })
            except Exception as e:
                print(f"  Error: {e}")
                results.append({
                    "name": name,
                    "retrieval_concurrent": r_conc,
                    "ingestion_concurrent": i_conc,
                    "error": str(e),
                })

            time.sleep(10)  # Cool down between tests

        # Print summary
        print("\n" + "=" * 60)
        print("Test Matrix Summary")
        print("=" * 60)
        print(f"{'Case':<6} {'R.Conc':<8} {'I.Conc':<8} {'R.QPS':<10} {'R.P99':<10} {'R.SR%':<8} {'I.DPM':<10} {'I.SR%':<8}")
        print("-" * 70)
        for r in results:
            if "error" in r:
                continue
            rm = r["result"]["retrieval"]
            im = r["result"]["ingestion"]
            print(f"{r['name']:<6} {r['retrieval_concurrent']:<8} {r['ingestion_concurrent']:<8} "
                  f"{rm.qps:<10.2f} {rm.latency_percentiles.p99:<10.2f} {rm.success_rate * 100:<8.2f} "
                  f"{im.throughput_per_sec * 60:<10.2f} {im.success_rate * 100:<8.2f}")

        return results


def main():
    parser = argparse.ArgumentParser(description="RAGFlow 混合负载测试")
    parser.add_argument("--retrieval-concurrent", type=int, default=10,
                        help="Number of concurrent retrieval requests")
    parser.add_argument("--ingestion-concurrent", type=int, default=4,
                        help="Number of concurrent ingestion threads")
    parser.add_argument("--duration", type=int, default=3,
                        help="Duration in minutes")
    parser.add_argument("--file-type", choices=["small", "medium", "large", "chunks"],
                        default="chunks", help="Type of files for ingestion")
    parser.add_argument("--test-matrix", action="store_true",
                        help="Run the predefined test matrix")
    parser.add_argument("--kb-id", type=str, default=None,
                        help="Knowledge base ID to test against (required, overrides config)")
    args = parser.parse_args()

    benchmark = MixedWorkloadBenchmark()

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

    if args.test_matrix:
        benchmark.run_test_matrix(args.duration, args.file_type)
    else:
        benchmark.run_mixed_test(
            args.retrieval_concurrent,
            args.ingestion_concurrent,
            args.duration,
            args.file_type
        )


if __name__ == "__main__":
    main()
