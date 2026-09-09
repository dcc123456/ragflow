#!/usr/bin/env python3
"""
RAGFlow 批量入库压测脚本

测试目标：文档上传、解析、分块、向量化与写入全流程的吞吐能力与系统稳定性

Usage:
    # 预热测试（不计入分析）
    python scripts/ingestion_test.py --warmup

    # 基准测试：单文件串行入库
    python scripts/ingestion_test.py --test-type baseline --file-type small

    # 梯度测试：并发上传
    python scripts/ingestion_test.py --test-type gradient --concurrency-levels 1,2,4,8,16,32,64

    # 稳定测试：持续上传30分钟
    python scripts/ingestion_test.py --test-type stability --duration 30 --concurrency 8
"""

import argparse
import glob
import os
import sys
import time
from pathlib import Path
from typing import Optional

# Add parent directory to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent))

from utils import config, RAGFlowApiClient, MetricsCollector, ResourceMonitor


class IngestionBenchmark:
    """批量入库压测主类"""

    def __init__(self):
        self.client = RAGFlowApiClient()
        self.kb_id = config.kb_id

    def get_files(self, file_type: str) -> list[str]:
        """获取指定类型的测试文件"""
        dir_map = {
            "small": config.small_pdfs_dir,
            "medium": config.medium_pdfs_dir,
            "large": config.large_pdfs_dir,
            "warmup": config.warmup_files_dir,
            "chunks": config.chunks_pdfs_dir,
        }
        target_dir = dir_map.get(file_type, config.small_pdfs_dir)
        pattern = os.path.join(target_dir, "*.pdf")
        files = glob.glob(pattern)
        if not files:
            print(f"Warning: No PDF files found in {pattern}")
        return sorted(files)

    def warmup(self) -> None:
        """预热测试"""
        print("=" * 60)
        print("Running warmup test (results not included in analysis)")
        print("=" * 60)

        warmup_files = self.get_files("warmup")
        if not warmup_files:
            # Fallback to small files if warmup dir is empty
            warmup_files = self.get_files("small")[:3]

        for i, file_path in enumerate(warmup_files, 1):
            print(f"[Warmup {i}/{len(warmup_files)}] Uploading: {os.path.basename(file_path)}")
            result = self.client.upload_document(self.kb_id, file_path)
            if result.success:
                print(f"  -> Success: {result.total_time_ms:.2f}ms")
            else:
                print(f"  -> Failed: {result.error}")
            time.sleep(1)

        print("Warmup completed.\n")

    def baseline_test(self, file_type: str = "small") -> dict:
        """基准测试：单文件串行入库，测量各阶段基线耗时"""
        print("=" * 60)
        print(f"Baseline Test - File type: {file_type}")
        print("=" * 60)

        files = self.get_files(file_type)
        if not files:
            return {}

        collector = MetricsCollector("ingestion_baseline")
        monitor = ResourceMonitor()
        monitor.start()

        for i, file_path in enumerate(files, 1):
            print(f"[{i}/{len(files)}] Uploading: {os.path.basename(file_path)}")
            result = self.client.upload_document(self.kb_id, file_path)

            stages_dict = {}
            if result.stages:
                stages_dict = {
                    "upload": result.stages.upload,
                    "parse": result.stages.parse,
                    "embedding": result.stages.embedding,
                    "indexing": result.stages.indexing,
                }

            collector.record_ingestion(file_path, result, result.total_time_ms, stages_dict)

            if result.success:
                print(f"  -> Success: {result.total_time_ms:.2f}ms")
                print(f"     Upload: {stages_dict.get('upload', 0):.2f}ms | "
                      f"Parse: {stages_dict.get('parse', 0):.2f}ms | "
                      f"Embedding: {stages_dict.get('embedding', 0):.2f}ms | "
                      f"Indexing: {stages_dict.get('indexing', 0):.2f}ms")
            else:
                print(f"  -> Failed: {result.error}")

        snapshots = monitor.stop()
        metrics = collector.compute_ingestion_metrics()

        print("\n" + "=" * 60)
        print("Baseline Test Results")
        print("=" * 60)
        self._print_ingestion_metrics(metrics)

        results_path = collector.save_results()
        print(f"\nResults saved to: {results_path}")

        return metrics

    def gradient_test(self, concurrency_levels: list[int], file_type: str = "small") -> list[dict]:
        """梯度测试：增加并发上传文件数"""
        print("=" * 60)
        print(f"Gradient Test - Concurrency levels: {concurrency_levels}")
        print("=" * 60)

        files = self.get_files(file_type)
        if not files:
            return []

        results = []
        for concurrency in concurrency_levels:
            print(f"\n{'=' * 40}")
            print(f"Testing with concurrency: {concurrency}")
            print(f"{'=' * 40}")

            collector = MetricsCollector(f"ingestion_gradient_c{concurrency}")
            monitor = ResourceMonitor()
            monitor.start()

            import concurrent.futures

            def upload_file(file_path: str) -> tuple:
                result = self.client.upload_document(self.kb_id, file_path)
                stages_dict = {}
                if result.stages:
                    stages_dict = {
                        "upload": result.stages.upload,
                        "parse": result.stages.parse,
                        "embedding": result.stages.embedding,
                        "indexing": result.stages.indexing,
                    }
                return file_path, result, result.total_time_ms, stages_dict

            with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as executor:
                futures = []
                for file_path in files:
                    futures.append(executor.submit(upload_file, file_path))

                for future in concurrent.futures.as_completed(futures):
                    try:
                        file_path, result, elapsed, stages = future.result()
                        collector.record_ingestion(file_path, result, elapsed, stages)
                        status = "SUCCESS" if result.success else "FAILED"
                        print(f"  [{status}] {os.path.basename(file_path)}: {elapsed:.2f}ms")
                    except Exception as e:
                        print(f"  [ERROR] {e}")

            snapshots = monitor.stop()
            metrics = collector.compute_ingestion_metrics()

            print(f"\nConcurrency {concurrency} Results:")
            self._print_ingestion_metrics(metrics)

            results.append({
                "concurrency": concurrency,
                "metrics": metrics,
            })

            time.sleep(5)  # Cool down between tests

        return results

    def stability_test(self, concurrency: int, duration_minutes: int, file_type: str = "small") -> dict:
        """稳定测试：在合适并发数下，持续上传文件"""
        print("=" * 60)
        print(f"Stability Test - Concurrency: {concurrency}, Duration: {duration_minutes}min")
        print("=" * 60)

        files = self.get_files(file_type)
        if not files:
            return {}

        collector = MetricsCollector("ingestion_stability")
        monitor = ResourceMonitor()
        monitor.start()

        start_time = time.time()
        duration_seconds = duration_minutes * 60
        file_index = 0
        total_uploaded = 0

        import concurrent.futures

        def upload_file(file_path: str) -> tuple:
            result = self.client.upload_document(self.kb_id, file_path)
            stages_dict = {}
            if result.stages:
                stages_dict = {
                    "upload": result.stages.upload,
                    "parse": result.stages.parse,
                    "embedding": result.stages.embedding,
                    "indexing": result.stages.indexing,
                }
            return file_path, result, result.total_time_ms, stages_dict

        with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as executor:
            while time.time() - start_time < duration_seconds:
                futures = []
                for i in range(concurrency):
                    file_path = files[file_index % len(files)]
                    file_index += 1
                    futures.append(executor.submit(upload_file, file_path))

                for future in concurrent.futures.as_completed(futures):
                    try:
                        file_path, result, elapsed, stages = future.result()
                        collector.record_ingestion(file_path, result, elapsed, stages)
                        total_uploaded += 1
                        if result.success:
                            print(f"\r  Uploaded: {total_uploaded}, Last: {elapsed:.2f}ms", end="", flush=True)
                        else:
                            print(f"\r  Uploaded: {total_uploaded}, Failed: {result.error}", end="", flush=True)
                    except Exception as e:
                        print(f"\n  [ERROR] {e}")

        print("\n")
        snapshots = monitor.stop()
        metrics = collector.compute_ingestion_metrics()

        print("\n" + "=" * 60)
        print("Stability Test Results")
        print("=" * 60)
        self._print_ingestion_metrics(metrics)

        results_path = collector.save_results()
        print(f"\nResults saved to: {results_path}")

        return metrics

    def _print_ingestion_metrics(self, metrics) -> None:
        """打印入库指标"""
        print(f"  Total docs:       {metrics.total_docs}")
        print(f"  Successful:       {metrics.successful_docs}")
        print(f"  Failed:           {metrics.failed_docs}")
        print(f"  Success rate:     {metrics.success_rate * 100:.2f}%")
        print(f"  Avg latency:      {metrics.avg_latency_ms:.2f}ms")
        print(f"  Throughput:       {metrics.throughput_per_sec:.4f} docs/sec")
        print(f"  Stage averages:")
        for stage, avg in metrics.stage_avg.items():
            print(f"    {stage}: {avg:.2f}ms")


def main():
    parser = argparse.ArgumentParser(description="RAGFlow 批量入库压测")
    parser.add_argument("--warmup", action="store_true", help="Run warmup test only")
    parser.add_argument("--test-type", choices=["baseline", "gradient", "stability"],
                        default="baseline", help="Type of test to run")
    parser.add_argument("--file-type", choices=["small", "medium", "large", "chunks"],
                        default="small", help="Type of test files to use")
    parser.add_argument("--concurrency-levels", type=str, default="1,2,4",
                        help="Comma-separated concurrency levels for gradient test")
    parser.add_argument("--concurrency", type=int, default=8,
                        help="Concurrency for stability test")
    parser.add_argument("--duration", type=int, default=30,
                        help="Duration in minutes for stability test")
    args = parser.parse_args()

    benchmark = IngestionBenchmark()

    # Auto-create knowledge base if needed
    configured_kb = config.kb_id
    if not configured_kb or configured_kb == "your-kb-id-here" or configured_kb == "auto":
        new_kb_id, resp = benchmark.client.get_or_create_knowledge_base(configured_kb or "auto")
        if resp.success:
            benchmark.kb_id = new_kb_id
            print(f"Created knowledge base: {new_kb_id}")
        else:
            print(f"Failed to create knowledge base: {resp.error}")
            sys.exit(1)
    else:
        new_kb_id, resp = benchmark.client.get_or_create_knowledge_base(configured_kb)
        if resp.success:
            benchmark.kb_id = new_kb_id

    # Check service health
    health = benchmark.client.health_check()
    if not health.success:
        print(f"Warning: RAGFlow service may not be healthy: {health.error}")

    if args.warmup:
        benchmark.warmup()
    elif args.test_type == "baseline":
        benchmark.baseline_test(args.file_type)
    elif args.test_type == "gradient":
        levels = [int(x) for x in args.concurrency_levels.split(",")]
        benchmark.gradient_test(levels, args.file_type)
    elif args.test_type == "stability":
        benchmark.stability_test(args.concurrency, args.duration, args.file_type)


if __name__ == "__main__":
    main()
