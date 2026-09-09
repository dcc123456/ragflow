#!/usr/bin/env python3
"""
RAGFlow 并发用户场景模拟测试脚本

测试目标：模拟真实用户行为模式，验证多用户并发访问下的端到端体验和系统表现

Usage:
    # 基础并发用户测试
    python scripts/concurrent_user_test.py --kb-id <kb_id> --concurrent-users 20 --duration 3

    # 测试矩阵（每组结束后等待该组未完成的入库任务收尾，最长 10 分钟）
    python scripts/concurrent_user_test.py --kb-id <kb_id> --run-matrix --drain-timeout 10

kb_id 只能通过 --kb-id 或配置文件（knowledge_base.kb_id）指定，脚本不会创建知识库。
"""

import argparse
import glob
import json
import os
import random
import sys
import threading
import time
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Optional

sys.path.insert(0, str(Path(__file__).parent.parent))

from utils import config, RAGFlowApiClient, MetricsCollector, ResourceMonitor


@dataclass
class UserSession:
    """Simulates a single user session with realistic behavior."""
    user_id: int
    kb_id: str
    client: RAGFlowApiClient
    think_time_min: float = 1.0  # Minimum think time between actions
    think_time_max: float = 5.0  # Maximum think time between actions

    # Metrics for this session
    total_requests: int = 0
    successful_requests: int = 0
    failed_requests: int = 0
    ttfr_ms: float = 0.0  # Time to first response
    session_start: Optional[float] = None
    last_request_time: Optional[float] = None
    error: Optional[str] = None  # Set when the session thread dies unexpectedly


class ConcurrentUserBenchmark:
    """并发用户场景模拟测试主类"""

    # Test matrix from requirements
    TEST_MATRIX = [
        {"users": 5, "duration": 3},
        {"users": 5, "duration": 10},
        {"users": 10, "duration": 3},
        {"users": 20, "duration": 3},
        {"users": 30, "duration": 3},
    ]

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

    def simulate_user_session(self, session: UserSession, duration_minutes: int,
                              collector: MetricsCollector, lock: threading.Lock) -> None:
        """Simulate a single user session with realistic behavior patterns."""
        session.session_start = time.time()
        start_time = session.session_start
        duration_seconds = duration_minutes * 60

        # First action - upload a document (time to first response)
        first_action_start = time.perf_counter()
        if self.files:
            file_path = random.choice(self.files)
            result = session.client.upload_document(session.kb_id, file_path)
            session.ttfr_ms = (time.perf_counter() - first_action_start) * 1000

            with lock:
                session.total_requests += 1
                if result.success:
                    session.successful_requests += 1
                else:
                    session.failed_requests += 1
                collector.record_ingestion(file_path, result, result.total_time_ms, {
                    "upload": result.stages.upload if result.stages else 0,
                    "parse": result.stages.parse if result.stages else 0,
                    "embedding": result.stages.embedding if result.stages else 0,
                    "indexing": result.stages.indexing if result.stages else 0,
                })

        # Subsequent actions - mix of retrieval and ingestion
        while time.time() - start_time < duration_seconds:
            # Random think time
            think_time = random.uniform(session.think_time_min, session.think_time_max)
            time.sleep(think_time)
            if time.time() - start_time >= duration_seconds:
                break

            # Mark the session as active *before* the request: an upload can take
            # longer than the monitoring window, the user is still active meanwhile.
            session.last_request_time = time.time()

            # Randomly choose action: 70% retrieval, 30% ingestion
            if random.random() < 0.7 and self.queries:
                # Retrieval action
                query = random.choice(self.queries)
                action_start = time.perf_counter()
                resp = session.client.retrieval(session.kb_id, query["text"])
                latency = (time.perf_counter() - action_start) * 1000

                with lock:
                    session.total_requests += 1
                    if resp.success:
                        session.successful_requests += 1
                    else:
                        session.failed_requests += 1
                    collector.record_retrieval(query["id"], latency, resp.success)
            elif self.files:
                # Ingestion action
                file_path = random.choice(self.files)
                action_start = time.perf_counter()
                result = session.client.upload_document(session.kb_id, file_path)
                latency = (time.perf_counter() - action_start) * 1000

                with lock:
                    session.total_requests += 1
                    if result.success:
                        session.successful_requests += 1
                    else:
                        session.failed_requests += 1
                    collector.record_ingestion(file_path, result, result.total_time_ms, {
                        "upload": result.stages.upload if result.stages else 0,
                        "parse": result.stages.parse if result.stages else 0,
                        "embedding": result.stages.embedding if result.stages else 0,
                        "indexing": result.stages.indexing if result.stages else 0,
                    })

    def run_concurrent_user_test(self, concurrent_users: int, duration_minutes: int,
                                  file_type: str = "chunks",
                                  drain_timeout_minutes: float = 10) -> dict:
        """运行并发用户场景模拟"""
        print("=" * 60)
        print(f"Concurrent User Simulation Test")
        print(f"  Concurrent users: {concurrent_users}")
        print(f"  Duration: {duration_minutes}min")
        print("=" * 60)

        self.load_test_data(file_type)

        collector = MetricsCollector(f"concurrent_users_{concurrent_users}")
        monitor = ResourceMonitor()
        monitor.start()

        start_time = time.time()
        lock = threading.Lock()
        sessions: list[UserSession] = []

        # Create user sessions
        for i in range(concurrent_users):
            client = RAGFlowApiClient()  # Each user gets their own client
            session = UserSession(
                user_id=i,
                kb_id=self.kb_id,
                client=client,
                think_time_min=0.5,
                think_time_max=3.0,
            )
            sessions.append(session)

        # Start all user sessions
        threads = []

        def run_session(session: UserSession) -> None:
            # A dead session thread would otherwise fail silently and just leave
            # the counters at zero, so record the error and report it.
            try:
                self.simulate_user_session(session, duration_minutes, collector, lock)
            except Exception as e:
                session.error = f"{type(e).__name__}: {e}"
                print(f"  [user {session.user_id}] session aborted: {session.error}")

        for session in sessions:
            t = threading.Thread(
                target=run_session,
                args=(session,),
                daemon=True
            )
            t.start()
            threads.append(t)

        # Monitor progress
        while time.time() - start_time < duration_minutes * 60:
            time.sleep(30)
            elapsed = time.time() - start_time
            with lock:
                total_reqs = sum(s.total_requests for s in sessions)
                total_succ = sum(s.successful_requests for s in sessions)
                total_fail = sum(s.failed_requests for s in sessions)
            active_users = sum(1 for s in sessions if s.last_request_time and
                             time.time() - s.last_request_time < 30)
            print(f"  [{elapsed / 60:.1f}/{duration_minutes}min] Users: {active_users}/{concurrent_users} | "
                  f"Requests: {total_reqs} | Success: {total_succ} | Failed: {total_fail}")

        # Wait for the requests that are still in flight (typically an upload waiting
        # for parsing) to finish, so that their results are part of the statistics.
        if drain_timeout_minutes > 0:
            print(f"\n  Waiting for in-flight requests to finish (timeout {drain_timeout_minutes}min)...")
            drain_deadline = time.time() + drain_timeout_minutes * 60
            for t in threads:
                remaining = drain_deadline - time.time()
                if remaining <= 0:
                    break
                t.join(remaining)
            unfinished = [s for t, s in zip(threads, sessions) if t.is_alive()]
            if unfinished:
                print(f"  Warning: {len(unfinished)} session(s) still running after the drain timeout; "
                      f"their in-flight requests are missing from the statistics.")
            else:
                print("  All requests finished.")

        snapshots = monitor.stop()
        retrieval_metrics = collector.compute_retrieval_metrics()
        ingestion_metrics = collector.compute_ingestion_metrics()
        resource_summary = monitor.get_summary()

        # Aggregate session metrics
        total_requests = sum(s.total_requests for s in sessions)
        successful_requests = sum(s.successful_requests for s in sessions)
        ttfr_list = [s.ttfr_ms for s in sessions if s.ttfr_ms > 0]
        avg_ttfr = sum(ttfr_list) / len(ttfr_list) if ttfr_list else 0

        print("\n" + "=" * 60)
        print("Concurrent User Test Results")
        print("=" * 60)

        aborted_sessions = [s for s in sessions if s.error]

        print("\n  Session Summary:")
        print(f"    Total users:       {concurrent_users}")
        print(f"    Active users:      {sum(1 for s in sessions if s.total_requests > 0)}")
        print(f"    Total requests:    {total_requests}")
        print(f"    Successful:        {successful_requests}")
        print(f"    Failed:            {total_requests - successful_requests}")
        print(f"    Success rate:      {successful_requests / total_requests * 100 if total_requests else 0:.2f}%")
        print(f"    Avg TTFR:          {avg_ttfr:.2f}ms")

        if aborted_sessions:
            print(f"    Aborted sessions:  {len(aborted_sessions)}")
            for s in aborted_sessions[:5]:
                print(f"      user {s.user_id}: {s.error}")
        if total_requests == 0:
            print("    Warning: no request completed within the test window. This usually means the "
                  "first upload of every session is still waiting for parsing to finish "
                  "(check the task queue backlog or increase --duration).")

        print("\n  Retrieval Metrics:")
        p = retrieval_metrics.latency_percentiles
        print(f"    Total queries:     {retrieval_metrics.total_queries}")
        print(f"    Success rate:      {retrieval_metrics.success_rate * 100:.2f}%")
        print(f"    QPS:               {retrieval_metrics.qps:.2f}")
        print(f"    Latency P50:       {p.p50:.2f}ms")
        print(f"    Latency P90:       {p.p90:.2f}ms")
        print(f"    Latency P99:       {p.p99:.2f}ms")

        print("\n  Ingestion Metrics:")
        print(f"    Total docs:        {ingestion_metrics.total_docs}")
        print(f"    Success rate:      {ingestion_metrics.success_rate * 100:.2f}%")
        print(f"    Throughput:        {ingestion_metrics.throughput_per_sec * 60:.2f} docs/min")

        print("\n  Resource Summary:")
        for container, stats in resource_summary.items():
            print(f"    {container}:")
            print(f"      CPU avg: {stats.get('cpu_avg', 0):.2f}% | max: {stats.get('cpu_max', 0):.2f}%")
            print(f"      Memory avg: {stats.get('memory_avg_mb', 0):.2f}MB | max: {stats.get('memory_max_mb', 0):.2f}MB")

        results_path = collector.save_results()
        print(f"\n  Results saved to: {results_path}")

        return {
            "concurrent_users": concurrent_users,
            "duration": duration_minutes,
            "total_requests": total_requests,
            "successful_requests": successful_requests,
            "avg_ttfr_ms": avg_ttfr,
            "retrieval_metrics": retrieval_metrics,
            "ingestion_metrics": ingestion_metrics,
            "resource_summary": resource_summary,
        }

    def run_test_matrix(self, file_type: str = "chunks", drain_timeout_minutes: float = 10) -> list[dict]:
        """运行测试矩阵"""
        print("=" * 60)
        print("Running Concurrent User Test Matrix")
        print("=" * 60)

        self.load_test_data(file_type)
        results = []

        for test_case in self.TEST_MATRIX:
            users = test_case["users"]
            duration = test_case["duration"]

            print(f"\n{'=' * 40}")
            print(f"Test: {users} users for {duration}min")
            print(f"{'=' * 40}")

            try:
                result = self.run_concurrent_user_test(users, duration, file_type, drain_timeout_minutes)
                results.append(result)
            except Exception as e:
                print(f"  Error: {e}")
                results.append({
                    "concurrent_users": users,
                    "duration": duration,
                    "error": str(e),
                })

            time.sleep(10)  # Cool down between tests

        # Print summary
        print("\n" + "=" * 60)
        print("Test Matrix Summary")
        print("=" * 60)
        print(f"{'Users':<8} {'Duration':<10} {'Requests':<12} {'Success%':<10} {'Avg TTFR':<12} {'QPS':<10}")
        print("-" * 70)
        for r in results:
            if "error" in r:
                continue
            rm = r.get("retrieval_metrics")
            total = r["total_requests"]
            success_pct = r["successful_requests"] / total * 100 if total else 0
            print(f"{r['concurrent_users']:<8} {r.get('duration', 'N/A'):<10} "
                  f"{total:<12} {success_pct:.2f} "
                  f"{r['avg_ttfr_ms']:<12.2f} {rm.qps if rm else 'N/A':<10.2f}")

        return results


def main():
    parser = argparse.ArgumentParser(description="RAGFlow 并发用户场景模拟测试")
    parser.add_argument("--concurrent-users", type=int, default=20,
                        help="Number of concurrent users")
    parser.add_argument("--duration", type=int, default=3,
                        help="Duration in minutes")
    parser.add_argument("--file-type", choices=["small", "medium", "large", "chunks"],
                        default="chunks", help="Type of files for ingestion")
    parser.add_argument("--run-matrix", action="store_true",
                        help="Run the predefined test matrix")
    parser.add_argument("--drain-timeout", type=float, default=10,
                        help="Max minutes to wait for the requests still in flight when the "
                             "test window ends to finish, before the statistics are computed "
                             "(default: 10, 0 disables the wait)")
    parser.add_argument("--kb-id", type=str, default=None,
                        help="Knowledge base ID to test against (required, overrides config)")
    args = parser.parse_args()

    benchmark = ConcurrentUserBenchmark()

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

    if args.run_matrix:
        benchmark.run_test_matrix(args.file_type, args.drain_timeout)
    else:
        benchmark.run_concurrent_user_test(args.concurrent_users, args.duration, args.file_type,
                                           args.drain_timeout)


if __name__ == "__main__":
    main()
