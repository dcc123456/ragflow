"""Configuration loader for RAGFlow benchmark tests."""

import os
from pathlib import Path
from typing import Any

import yaml


class ConfigLoader:
    """Loads and provides access to benchmark configuration."""

    _instance = None
    _config: dict[str, Any] = {}

    def __new__(cls):
        if cls._instance is None:
            cls._instance = super().__new__(cls)
            cls._instance._load_config()
        return cls._instance

    def _load_config(self) -> None:
        """Load configuration from YAML file."""
        config_path = os.environ.get(
            "BENCHMARK_CONFIG",
            Path(__file__).parent.parent / "config" / "benchmark_config.yaml"
        )
        with open(config_path, "r", encoding="utf-8") as f:
            self._config = yaml.safe_load(f)

    def get(self, key: str, default: Any = None) -> Any:
        """Get configuration value by dot-notation key."""
        keys = key.split(".")
        value = self._config
        for k in keys:
            if isinstance(value, dict):
                value = value.get(k)
            else:
                return default
            if value is None:
                return default
        return value

    @property
    def ragflow_base_url(self) -> str:
        return self.get("ragflow.base_url", "http://127.0.0.1:9380")

    @property
    def ragflow_api_key(self) -> str:
        return self.get("ragflow.api_key", "")

    @property
    def ragflow_timeout(self) -> int:
        return self.get("ragflow.timeout", 300)

    @property
    def kb_id(self) -> str:
        return self.get("knowledge_base.kb_id", "")

    @property
    def small_pdfs_dir(self) -> str:
        return self.get("test_data.small_pdfs_dir", "")

    @property
    def medium_pdfs_dir(self) -> str:
        return self.get("test_data.medium_pdfs_dir", "")

    @property
    def large_pdfs_dir(self) -> str:
        return self.get("test_data.large_pdfs_dir", "")

    @property
    def warmup_files_dir(self) -> str:
        return self.get("test_data.warmup_files_dir", "")

    @property
    def chunks_pdfs_dir(self) -> str:
        return self.get("test_data.chunks_pdfs_dir", "")

    @property
    def query_file(self) -> str:
        return self.get("test_data.query_file", "")

    @property
    def results_dir(self) -> str:
        return self.get("results.output_dir", "./results")

    @property
    def monitoring_enabled(self) -> bool:
        return self.get("monitoring.enabled", True)

    @property
    def monitoring_interval(self) -> int:
        return self.get("monitoring.interval_seconds", 5)


config = ConfigLoader()
