from __future__ import annotations

from enum import Enum


class EvaluationMetric(str, Enum):
    FAITHFULNESS = "faithfulness"
    CONTEXT_RELEVANCE = "context_relevance"
    SEMANTIC_SIMILARITY = "semantic_similarity"


class EvaluationRunStatus(str, Enum):
    PENDING = "PENDING"
    RUNNING = "RUNNING"
    COMPLETED = "COMPLETED"
    FAILED = "FAILED"
    CANCEL = "CANCEL"


DEFAULT_EVALUATION_METRICS: tuple[EvaluationMetric, ...] = (
    EvaluationMetric.FAITHFULNESS,
    EvaluationMetric.CONTEXT_RELEVANCE,
    EvaluationMetric.SEMANTIC_SIMILARITY,
)
