"""RAGFlow API client for benchmark tests."""

import re
import time
from dataclasses import dataclass, field
from typing import Any

import requests

from .config_loader import config


@dataclass
class ApiResponse:
    """Standard API response wrapper."""
    success: bool
    data: Any = None
    error: str = ""
    elapsed_ms: float = 0.0


@dataclass
class IngestionStage:
    """Timings for each ingestion stage."""
    upload: float = 0.0
    parse: float = 0.0
    chunk: float = 0.0
    embedding: float = 0.0
    indexing: float = 0.0


@dataclass
class IngestionResult:
    """Result of a document ingestion operation."""
    doc_id: str = ""
    success: bool = False
    total_time_ms: float = 0.0
    stages: IngestionStage = field(default_factory=IngestionStage)
    error: str = ""


class RAGFlowApiClient:
    """Client for interacting with RAGFlow API."""

    # "<HH:MM:SS> Page(<from>~<to>): <message>"
    _PROGRESS_LINE_RE = re.compile(
        r"^\s*(?:(?P<ts>\d{2}:\d{2}:\d{2})\s+)?"
        r"(?:Page\((?P<from_page>\d+)~(?P<to_page>\d+)\):\s*)?"
        r"(?P<msg>.*)$"
    )
    _DURATION_RE = re.compile(r"\((\d+(?:\.\d+)?)\s*s\)")
    _SECONDS_PER_DAY = 86400.0

    def __init__(self):
        self.base_url = config.ragflow_base_url.rstrip("/")
        self.api_key = config.ragflow_api_key
        self.timeout = config.ragflow_timeout
        self.session = requests.Session()
        self.session.headers.update({"Authorization": f"Bearer {self.api_key}"})

    def _request(self, method: str, path: str, **kwargs) -> ApiResponse:
        """Make HTTP request with timing."""
        url = f"{self.base_url}{path}"
        start = time.perf_counter()
        try:
            resp = self.session.request(method, url, timeout=self.timeout, **kwargs)
            elapsed = (time.perf_counter() - start) * 1000
            if resp.status_code == 200:
                return ApiResponse(success=True, data=resp.json(), elapsed_ms=elapsed)
            else:
                return ApiResponse(success=False, error=resp.text, elapsed_ms=elapsed)
        except Exception as e:
            elapsed = (time.perf_counter() - start) * 1000
            return ApiResponse(success=False, error=str(e), elapsed_ms=elapsed)

    def upload_document(self, kb_id: str, file_path: str) -> IngestionResult:
        """Upload and process a document, returning detailed timing."""
        start = time.perf_counter()
        stages = IngestionStage()

        # Upload stage with retry for transient errors (exponential backoff)
        max_upload_retries = 3
        for upload_attempt in range(max_upload_retries + 1):
            t0 = time.perf_counter()
            with open(file_path, "rb") as f:
                files = {"file": (file_path.split("/")[-1], f)}
                resp = self._request("POST", f"/api/v1/datasets/{kb_id}/documents", files=files)
            stages.upload = (time.perf_counter() - t0) * 1000

            if not resp.success:
                error_msg = resp.error
                # Check for transient database errors
                if "transaction is open" in error_msg or "database" in error_msg.lower():
                    if upload_attempt < max_upload_retries:
                        time.sleep((upload_attempt + 1) * 2)
                        continue
                return IngestionResult(success=False, total_time_ms=(time.perf_counter() - start) * 1000, error=error_msg)

            # Check if response has valid data with document ID
            doc_list = resp.data.get("data", []) if resp.data else []
            if not doc_list:
                # Check if it's a transient error
                msg = str(resp.data) if resp.data else ""
                if "transaction" in msg or "database" in msg.lower():
                    if upload_attempt < max_upload_retries:
                        time.sleep((upload_attempt + 1) * 2)
                        continue
                return IngestionResult(success=False, total_time_ms=(time.perf_counter() - start) * 1000, error=f"Empty upload response: {resp.data}")

            doc_id = doc_list[0].get("id") if doc_list else None
            if not doc_id:
                if upload_attempt < max_upload_retries:
                    time.sleep((upload_attempt + 1) * 2)
                    continue
                return IngestionResult(success=False, total_time_ms=(time.perf_counter() - start) * 1000, error=f"No doc_id in response: {resp.data}")

            break

        # Trigger document parsing/processing with retry (exponential backoff)
        max_retries = 2
        for attempt in range(max_retries + 1):
            parse_resp = self._request("POST", f"/api/v1/datasets/{kb_id}/documents/parse", json={"document_ids": [doc_id]})
            if not parse_resp.success:
                if attempt < max_retries:
                    time.sleep((attempt + 1) * 2)
                    continue
                return IngestionResult(doc_id=doc_id, success=False, total_time_ms=(time.perf_counter() - start) * 1000, error=f"Parse trigger failed: {parse_resp.error}")

            # Parse + chunk + vectorize + write (polling for completion)
            # Status is retrieved via the list documents endpoint with doc_ids filter
            t1 = time.perf_counter()
            poll_count = 0
            parse_failed = False

            while True:
                # Use list endpoint with doc_ids filter to get document status
                status_resp = self._request("GET", f"/api/v1/datasets/{kb_id}/documents?doc_ids={doc_id}&page=1&page_size=1")
                poll_count += 1
                if status_resp.success:
                    docs = status_resp.data.get("data", {}).get("docs", []) if status_resp.data else []
                    if docs:
                        run_status = docs[0].get("run", "0")
                        progress_msg = docs[0].get("progress_msg", "") or ""
                        if run_status in ("3", "DONE"):  # DONE
                            # Parse progress_msg to extract stage timings
                            stages = self._parse_progress_message(progress_msg, stages)
                            break
                        elif run_status in ("4", "FAIL"):  # FAIL
                            parse_failed = True
                            break
                if (time.perf_counter() - t1) * 1000 > self.timeout * 1000:
                    parse_failed = True
                    break
                time.sleep(2)

            if not parse_failed:
                break

            # Retry if failed
            if attempt < max_retries:
                time.sleep((attempt + 1) * 2)
                continue

            return IngestionResult(doc_id=doc_id, success=False, total_time_ms=(time.perf_counter() - start) * 1000, error="Processing failed")

        total_time = (time.perf_counter() - start) * 1000

        return IngestionResult(doc_id=doc_id, success=True, total_time_ms=total_time, stages=stages)

    @staticmethod
    def _timestamp_to_seconds(ts: str | None) -> float | None:
        """Convert an "HH:MM:SS" progress timestamp into seconds since midnight."""
        if not ts:
            return None
        try:
            hours, minutes, seconds = (int(part) for part in ts.split(":"))
        except ValueError:
            return None
        return hours * 3600 + minutes * 60 + seconds

    @staticmethod
    def _merge_intervals(intervals: list[tuple[float, float]]) -> float:
        """Total wall clock time covered by the given intervals.

        Intervals that overlap (i.e. page ranges processed in parallel) are only
        counted once, so the result is the real elapsed time instead of the sum
        of every segment's own duration.
        """
        merged: list[list[float]] = []
        for start, end in sorted((s, e) for s, e in intervals if e > s):
            if merged and start <= merged[-1][1]:
                merged[-1][1] = max(merged[-1][1], end)
            else:
                merged.append([start, end])
        return sum(end - start for start, end in merged)

    def _parse_progress_message(self, msg: str, stages: IngestionStage) -> IngestionStage:
        """Parse progress_msg to extract stage timings.

        A document is split into page ranges ("Page(1~13): ...") which are parsed
        **in parallel**, so their durations overlap and must not be summed: two
        segments finishing in 75s each only consume ~75s of wall clock time, not
        150s.

        Every progress line carries an "HH:MM:SS" timestamp and every duration is
        reported when the step *finishes*, so the real cost of a stage is obtained
        by merging the time intervals of all the segments running it concurrently:

        - parse     : [ts of "Start to parse.", ts of "Finish parsing."]
        - embedding : [ts of "Embedding chunks (Xs)" - X, ts]
        - indexing  : [ts of "Indexing done (Xs)" - X, ts]

        If no timestamp is available at all, fall back to summing the per segment
        durations reported by the server.
        """
        events: list[dict[str, Any]] = []
        for raw_line in msg.split("\n"):
            line = raw_line.strip()
            if not line:
                continue
            matched = self._PROGRESS_LINE_RE.match(line)
            if not matched:
                continue
            text = matched.group("msg") or ""
            duration = self._DURATION_RE.search(text)
            events.append({
                "ts": self._timestamp_to_seconds(matched.group("ts")),
                "segment": (f"{matched.group('from_page')}~{matched.group('to_page')}"
                            if matched.group("from_page") else ""),
                "msg": text,
                "duration": float(duration.group(1)) if duration else None,
            })

        if not any(e["ts"] is not None for e in events):
            self._parse_progress_message_sequential(events, stages)
            return stages

        # Wall clock interval of every page range segment: the parse stage runs
        # from "Start to parse." until "Finish parsing.".
        parse_start: dict[str, float] = {}
        parse_end: dict[str, float] = {}
        segment_first: dict[str, float] = {}
        segment_last: dict[str, float] = {}
        embedding_intervals: list[tuple[float, float]] = []
        indexing_intervals: list[tuple[float, float]] = []

        for event in events:
            ts = event["ts"]
            if ts is None:
                continue
            segment = event["segment"]
            text = event["msg"]

            # Fallbacks for parsers that do not emit the start/end markers.
            segment_first[segment] = min(segment_first.get(segment, ts), ts)
            segment_last[segment] = max(segment_last.get(segment, ts), ts)

            if "Start to parse" in text:
                parse_start[segment] = min(parse_start.get(segment, ts), ts)
            elif "Finish parsing" in text:
                parse_end[segment] = max(parse_end.get(segment, ts), ts)

            if event["duration"] is None:
                continue
            if "Embedding chunks" in text:
                embedding_intervals.append((ts - event["duration"], ts))
            elif "Indexing done" in text:
                indexing_intervals.append((ts - event["duration"], ts))

        parse_intervals: list[tuple[float, float]] = []
        for segment, last in segment_last.items():
            start = parse_start.get(segment, segment_first[segment])
            end = parse_end.get(segment, last)
            if end < start:  # crossed midnight
                end += self._SECONDS_PER_DAY
            parse_intervals.append((start, end))

        stages.parse = self._merge_intervals(parse_intervals) * 1000  # Convert to ms
        stages.embedding = self._merge_intervals(embedding_intervals) * 1000
        stages.indexing = self._merge_intervals(indexing_intervals) * 1000
        stages.chunk = 0  # Not tracked separately

        return stages

    def _parse_progress_message_sequential(self, events: list[dict[str, Any]],
                                           stages: IngestionStage) -> IngestionStage:
        """Fallback used when progress_msg carries no timestamps: sum the durations.

        The result overestimates the parse time whenever page ranges are processed
        in parallel, but it is the best guess available.
        """
        vectorize_time = 0.0
        write_time = 0.0
        total_task_time = 0.0

        for event in events:
            duration = event["duration"]
            if duration is None:
                continue
            if "Embedding chunks" in event["msg"]:
                vectorize_time += duration
            elif "Indexing done" in event["msg"]:
                write_time += duration
            elif "Task done" in event["msg"]:
                total_task_time += duration

        parse_time = max(total_task_time - vectorize_time - write_time, 0.0)

        stages.parse = parse_time * 1000
        stages.embedding = vectorize_time * 1000
        stages.indexing = write_time * 1000
        stages.chunk = 0

        return stages

    def retrieval(self, kb_id: str, query: str) -> ApiResponse:
        """Execute retrieval query."""
        return self._request("POST", "/api/v1/retrieval", json={"knowledge_base_id": kb_id, "query": query})

    def health_check(self) -> ApiResponse:
        """Check RAGFlow service health."""
        # Try the main health endpoint first
        resp = self._request("GET", "/api/v1/system/healthz")
        if resp.success or resp.elapsed_ms > 0:
            return resp
        # Fallback to simple liveness probe
        return self._request("GET", "/healthz")

    def create_knowledge_base(self, name: str, description: str = "") -> ApiResponse:
        """Create a new knowledge base (dataset)."""
        return self._request("POST", "/api/v1/datasets", json={
            "name": name,
            "description": description,
            "permission": "me",
        })

    def list_knowledge_bases(self) -> ApiResponse:
        """List all knowledge bases."""
        return self._request("GET", "/api/v1/datasets")

    def get_knowledge_base(self, kb_id: str) -> ApiResponse:
        """Fetch an existing knowledge base by id. Never creates one."""
        return self._request("GET", f"/api/v1/datasets/{kb_id}")

    def get_or_create_knowledge_base(self, kb_id: str, name: str = None) -> tuple[str, ApiResponse]:
        """Get existing knowledge base or create a new one.

        Returns:
            tuple of (kb_id, response) - kb_id may be auto-generated if original was "auto"
        """
        # If configured KB is a placeholder, always create a new one
        if kb_id in ("your-kb-id-here", "", "auto", None) or not kb_id:
            kb_name = name or f"benchmark_kb_{int(time.time())}"
            resp = self.create_knowledge_base(kb_name, "Auto-created by benchmark test")
            if resp.success:
                data = resp.data.get("data", {}) if resp.data else {}
                new_kb_id = data.get("id") or data.get("kb_id") or ""
                if new_kb_id:
                    return new_kb_id, resp
            return "", resp

        # Try to get existing KB
        resp = self._request("GET", f"/api/v1/datasets/{kb_id}")
        if resp.success:
            return kb_id, resp

        # KB doesn't exist, create new one
        kb_name = name or f"benchmark_kb_{int(time.time())}"
        resp = self.create_knowledge_base(kb_name, "Auto-created by benchmark test")
        if resp.success:
            data = resp.data.get("data", {}) if resp.data else {}
            new_kb_id = data.get("id") or data.get("kb_id") or kb_id
            return new_kb_id, resp
        return kb_id, resp
