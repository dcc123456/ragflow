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
"""
Shadow Write Proxy - Executes database operations on a primary database while
simultaneously executing on shadow databases for comparison and logging.

The primary database's result is always returned immediately. Shadow databases'
results are compared asynchronously in the background and mismatches are logged.
Shadow database errors are ignored.
"""

import logging
import time
from concurrent.futures import ThreadPoolExecutor
from typing import Any, Callable

from common.doc_store.doc_store_base import DocStoreConnection, OrderByExpr, MatchExpr


def _format_elapsed(seconds: float) -> str:
    """Format elapsed time in human-readable format."""
    if seconds < 1:
        return f"{seconds * 1000:.0f}ms"
    elif seconds < 60:
        return f"{seconds:.1f}s"
    elif seconds < 3600:
        mins, secs = divmod(seconds, 60)
        return f"{int(mins)}m {secs:.0f}s"
    else:
        hours, remainder = divmod(seconds, 3600)
        mins, secs = divmod(remainder, 60)
        return f"{int(hours)}h {int(mins)}m {int(secs)}s"


def _truncate_str(s: Any, max_len: int = 200) -> str:
    """Truncate string representation for logging."""
    s_str = str(s)
    if len(s_str) > max_len:
        return s_str[:max_len] + "..."
    return s_str


def _compare_results(result1: Any, result2: Any, method_name: str) -> list[str]:
    """
    Compare two results and return list of differences.
    Returns empty list if results are considered equivalent.
    """
    differences = []

    # Handle None cases
    if result1 is None and result2 is None:
        return []
    if result1 is None or result2 is None:
        differences.append(f"One result is None: primary={result1 is not None}, shadow={result2 is not None}")
        return differences

    # For methods that return bool
    if isinstance(result1, bool) and isinstance(result2, bool):
        if result1 != result2:
            differences.append(f"Boolean mismatch: primary={result1}, shadow={result2}")
        return differences

    # For methods that return int (like delete count)
    if isinstance(result1, int) and isinstance(result2, int):
        if result1 != result2:
            differences.append(f"Integer mismatch: primary={result1}, shadow={result2}")
        return differences

    # For methods that return list (like insert errors)
    if isinstance(result1, list) and isinstance(result2, list):
        if result1 != result2:
            differences.append(f"List mismatch: primary len={len(result1)}, shadow len={len(result2)}")
            if result1 and result2:
                differences.append(f"  primary sample: {_truncate_str(result1[:3])}")
                differences.append(f"  shadow sample: {_truncate_str(result2[:3])}")
        return differences

    # For dict results (like get, health)
    if isinstance(result1, dict) and isinstance(result2, dict):
        keys1, keys2 = set(result1.keys()), set(result2.keys())
        if keys1 != keys2:
            differences.append(f"Dict keys mismatch: primary has {keys1 - keys2}, shadow has {keys2 - keys1}")
        # Don't compare values deeply for complex dicts
        return differences

    # For tuple results (like Infinity search returns DataFrame and count)
    if isinstance(result1, tuple) and isinstance(result2, tuple):
        if len(result1) != len(result2):
            differences.append(f"Tuple length mismatch: primary={len(result1)}, shadow={len(result2)}")
            return differences
        # Compare second element (count) if it's an int
        if len(result1) >= 2 and isinstance(result1[1], int) and isinstance(result2[1], int):
            if result1[1] != result2[1]:
                differences.append(f"Count mismatch: primary={result1[1]}, shadow={result2[1]}")
        # For DataFrame comparison, check row count
        if len(result1) >= 1 and hasattr(result1[0], "__len__") and hasattr(result2[0], "__len__"):
            try:
                len1, len2 = len(result1[0]), len(result2[0])
                if len1 != len2:
                    differences.append(f"DataFrame row count mismatch: primary={len1}, shadow={len2}")
            except Exception:
                pass
        return differences

    # For objects with __len__ (like DataFrame)
    try:
        len1, len2 = len(result1), len(result2)
        if len1 != len2:
            differences.append(f"Length mismatch: primary={len1}, shadow={len2}")
    except Exception:
        pass

    return differences


# Module-level background executor for shadow comparisons
# Using a dedicated thread pool to avoid blocking the main thread
_shadow_executor = ThreadPoolExecutor(max_workers=4, thread_name_prefix="shadow_compare")
_shadow_executor_started = False


def _ensure_executor_started():
    """Ensure the shadow executor is started. Called once at module load."""
    global _shadow_executor_started
    if not _shadow_executor_started:
        _shadow_executor_started = True


_ensure_executor_started()


class ShadowWriteProxy(DocStoreConnection):
    """
    A proxy that executes operations on a primary database and one or more shadow databases.

    - Primary database result is always returned immediately
    - Shadow databases execute in background asynchronously
    - Shadow database errors are caught and logged, never raised
    - Mismatches between primary and shadow results are logged as warnings
    """

    def __init__(self, primary_conn: DocStoreConnection, shadow_conns: list[DocStoreConnection]):
        """
        Initialize the shadow write proxy.

        Args:
            primary_conn: The primary database connection (authoritative)
            shadow_conns: List of shadow database connections (for comparison)
        """
        self.primary = primary_conn
        self.shadows = shadow_conns
        self.logger = logging.getLogger("ragflow.shadow_write_proxy")

        if self.shadows:
            shadow_types = [s.db_type() for s in self.shadows]
            self.logger.info(f"ShadowWriteProxy initialized with primary={primary_conn.db_type()}, shadows={shadow_types}")
        else:
            self.logger.info(f"ShadowWriteProxy initialized with primary={primary_conn.db_type()}, no shadows")

    def _execute_on_shadow(self, shadow_idx: int, shadow_conn: DocStoreConnection, method: Callable, method_name: str, args: tuple, kwargs: dict) -> tuple[int, Any, Exception | None]:
        """
        Execute a method on a shadow connection, catching any exceptions.

        Returns:
            Tuple of (shadow_index, result, exception)
        """
        try:
            result = method(*args, **kwargs)
            return (shadow_idx, result, None)
        except Exception as e:
            return (shadow_idx, None, e)

    def _compare_shadows_async(self, method_name: str, primary_result: Any, args: tuple, kwargs: dict, start_time: float):
        """
        Asynchronously compare shadow results with primary result.
        This runs in a background thread and logs any mismatches or errors.

        Args:
            method_name: Name of the method being executed
            primary_result: Result from primary database
            args: Positional arguments passed to the method
            kwargs: Keyword arguments passed to the method
            start_time: Timestamp when the primary operation started (for logging elapsed time)
        """
        for idx, shadow in enumerate(self.shadows):
            shadow_method = getattr(shadow, method_name, None)
            if shadow_method is None:
                self.logger.warning(f"[{_format_elapsed(time.time() - start_time)}] Shadow {idx} ({shadow.db_type()}) does not have method '{method_name}'")
                continue

            try:
                shadow_result = shadow_method(*args, **kwargs)
                differences = _compare_results(primary_result, shadow_result, method_name)
                if differences:
                    self.logger.warning(
                        f"[{_format_elapsed(time.time() - start_time)}] Result mismatch in {method_name} between primary ({self.primary.db_type()}) "
                        f"and shadow {idx} ({shadow.db_type()}):\n" + "\n".join(f"  - {d}" for d in differences)
                    )
            except Exception as e:
                self.logger.warning(f"[{_format_elapsed(time.time() - start_time)}] Shadow database {idx} ({shadow.db_type()}) error in {method_name}: {type(e).__name__}: {e}")

    def _execute_with_shadows(self, method_name: str, *args, **kwargs) -> Any:
        """
        Execute a method on primary first, then asynchronously on shadows.
        Returns primary result immediately, shadows compare in background.
        """
        start_time = time.time()

        # Get the method from primary
        primary_method = getattr(self.primary, method_name)

        # Execute on primary first (must succeed)
        try:
            primary_result = primary_method(*args, **kwargs)
        except Exception as e:
            self.logger.error(f"[{_format_elapsed(time.time() - start_time)}] Primary database ({self.primary.db_type()}) error in {method_name}: {e}")
            raise

        # If no shadows, return immediately
        if not self.shadows:
            return primary_result

        # Submit shadow comparison to background executor
        # The primary result is captured by reference, so no copy is made
        _shadow_executor.submit(self._compare_shadows_async, method_name, primary_result, args, kwargs, start_time)

        # Return primary result immediately
        return primary_result

    # ==================== Database Operations ====================

    def db_type(self) -> str:
        return self.primary.db_type()

    def health(self) -> dict:
        # Execute health check on primary
        primary_health = self.primary.health()

        # Also check shadow health but don't affect result
        for idx, shadow in enumerate(self.shadows):
            try:
                shadow_health = shadow.health()
                self.logger.debug(f"Shadow {idx} ({shadow.db_type()}) health: {shadow_health}")
            except Exception as e:
                self.logger.warning(f"Shadow {idx} ({shadow.db_type()}) health check failed: {e}")

        return primary_health

    # ==================== Table Operations ====================

    def create_idx(self, index_name: str, dataset_id: str, vector_size: int, parser_id: str = None):
        return self._execute_with_shadows("create_idx", index_name, dataset_id, vector_size, parser_id)

    def create_doc_meta_idx(self, index_name: str):
        return self._execute_with_shadows("create_doc_meta_idx", index_name)

    def delete_idx(self, index_name: str, dataset_id: str):
        return self._execute_with_shadows("delete_idx", index_name, dataset_id)

    def index_exist(self, index_name: str, dataset_id: str) -> bool:
        return self._execute_with_shadows("index_exist", index_name, dataset_id)

    # ==================== CRUD Operations ====================

    def search(
        self,
        select_fields: list[str],
        highlight_fields: list[str],
        condition: dict,
        match_expressions: list[MatchExpr],
        order_by: OrderByExpr,
        offset: int,
        limit: int,
        index_names: str | list[str],
        knowledgebase_ids: list[str],
        agg_fields: list[str] | None = None,
        rank_feature: dict | None = None,
    ):
        return self._execute_with_shadows("search", select_fields, highlight_fields, condition, match_expressions, order_by, offset, limit, index_names, knowledgebase_ids, agg_fields, rank_feature)

    def get(self, data_id: str, index_name: str, knowledgebase_ids: list[str]) -> dict | None:
        return self._execute_with_shadows("get", data_id, index_name, knowledgebase_ids)

    def insert(self, rows: list[dict], index_name: str, dataset_id: str = None) -> list[str]:
        return self._execute_with_shadows("insert", rows, index_name, dataset_id)

    def update(self, condition: dict, new_value: dict, index_name: str, dataset_id: str) -> bool:
        return self._execute_with_shadows("update", condition, new_value, index_name, dataset_id)

    def delete(self, condition: dict, index_name: str, dataset_id: str) -> int:
        return self._execute_with_shadows("delete", condition, index_name, dataset_id)

    # ==================== Helper Functions ====================

    def get_total(self, res):
        return self.primary.get_total(res)

    def get_doc_ids(self, res):
        return self.primary.get_doc_ids(res)

    def get_fields(self, res, fields: list[str]) -> dict[str, dict]:
        return self.primary.get_fields(res, fields)

    def get_highlight(self, res, keywords: list[str], field_name: str):
        return self.primary.get_highlight(res, keywords, field_name)

    def get_aggregation(self, res, field_name: str):
        return self.primary.get_aggregation(res, field_name)

    # ==================== SQL ====================

    def sql(self, sql: str, fetch_size: int, format: str):
        return self._execute_with_shadows("sql", sql, fetch_size, format)
