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
RAG Evaluation Service

Provides functionality for evaluating RAG system performance including:
- Collection management
- Test case management
- Evaluation execution
- Metrics computation
- Configuration recommendations
"""

import asyncio
import logging
import math
import os
import queue
import threading
from typing import List, Dict, Any, Optional, Tuple, Union, Sequence
from timeit import default_timer as timer

from peewee import fn

from api.constants import DATASET_NAME_LIMIT
from api.db.db_models import DB, Dialog, EvaluationCollection, EvaluationCase, EvaluationRun, EvaluationResult
from api.db.db_utils import bulk_insert_into_db
from api.db.services.common_service import CommonService
from api.db.services import duplicate_name
from api.db.services.dialog_service import DialogService
from api.db.services.document_service import DocumentService, queue_reembedding_dup_tasks
from api.db.services.llm_service import LLMBundle
from api.db.services.user_service import TenantService
from api.db.joint_services.tenant_model_service import get_model_config_from_provider_instance, get_model_type_by_name
from common.misc_utils import get_uuid
from common.string_utils import remove_redundant_spaces
from common.time_utils import current_timestamp
from common.constants import StatusEnum, LLMType
from common.evaluation_metrics import EvaluationMetric, EvaluationRunStatus, DEFAULT_EVALUATION_METRICS


class EvaluationService(CommonService):
    """Service for managing RAG evaluations"""

    model = EvaluationCollection

    @staticmethod
    def _next_run_version(collection_id: str, target_type: str, target_id: str, base_name: str) -> int:
        prefix = f"{base_name}_v"
        max_version = 0
        runs = EvaluationRun.select(EvaluationRun.name).where(
            (EvaluationRun.collection_id == collection_id) &
            (EvaluationRun.target_type == target_type) &
            (EvaluationRun.target_id == target_id) &
            (EvaluationRun.name.startswith(prefix))
        )
        for run in runs:
            name = run.name or ""
            if not name.startswith(prefix):
                continue
            version_str = name[len(prefix):]
            if version_str.isdigit():
                max_version = max(max_version, int(version_str))
        return max_version + 1

    # ==================== Collection Management ====================

    @classmethod
    @DB.connection_context()
    def create_collection(cls, name: str, description: str,
                      tenant_id: str, user_id: str, target_type: str = "chat") -> Tuple[bool, str]:
        """
        Create a new evaluation collection.

        Args:
            name: Collection name
            description: Collection description
            tenant_id: Tenant ID
            user_id: User ID who creates the collection
            target_type: chat|agent

        Returns:
            (success, collection_id or error_message)
        """
        try:
            timestamp = current_timestamp()
            collection_id = get_uuid()
            target_type = (target_type or "chat").strip().lower()
            if target_type not in {"chat", "agent"}:
                return False, "target_type must be 'chat' or 'agent'"
            # Validate name
            if not isinstance(name, str):
                return False, "Evaluation collection name must be string."
            collection_name = name.strip()
            if collection_name == "":
                return False, "Evaluation collection name can't be empty."
            if len(collection_name.encode("utf-8")) > DATASET_NAME_LIMIT:
                return False, f"Evaluation collection name length is {len(collection_name)} which is larger than {DATASET_NAME_LIMIT}"
            collection_name = duplicate_name(
                cls.query,
                name=collection_name,
                tenant_id=tenant_id,
                status=StatusEnum.VALID.value,
            )
            collection = {
                "id": collection_id,
                "tenant_id": tenant_id,
                "target_type": target_type,
                "name": collection_name,
                "description": description,
                "created_by": user_id,
                "create_time": timestamp,
                "update_time": timestamp,
                "status": StatusEnum.VALID.value
            }

            if not cls.model.create(**collection):
                return False, "Failed to create collection"

            return True, collection_id
        except Exception as e:
            logging.error(f"Error creating evaluation collection: {e}")
            return False, str(e)

    @classmethod
    @DB.connection_context()
    def list_collections(cls, tenant_id: str, user_id: str,
                     page: int = 1, page_size: int = 20, keywords: str = "") -> Dict[str, Any]:
        """List collections for a tenant"""
        try:
            query = cls.model.select().where(
                (cls.model.tenant_id == tenant_id) &
                (cls.model.status == StatusEnum.VALID.value)
            ).order_by(cls.model.create_time.desc())

            keywords = (keywords or "").strip()
            if keywords:
                query = query.where(fn.LOWER(cls.model.name).contains(keywords.lower()))

            total = query.count()
            collections = query.paginate(page, page_size)

            return {
                "total": total,
                "collections": [c.to_dict() for c in collections]
            }
        except Exception as e:
            logging.error(f"Error listing collections: {e}")
            return {"total": 0, "collections": []}

    @classmethod
    @DB.connection_context()
    def update_collection(cls, collection_id: str, **kwargs) -> bool:
        """Update collection with name de-duplication"""
        try:
            ok, collection = cls.get_by_id(collection_id)
            if not ok or not collection:
                return False

            if "name" in kwargs:
                name = kwargs.get("name", "")
                if isinstance(name, str):
                    name = name.strip()
                if name and name != collection.name:
                    name = duplicate_name(
                        cls.query,
                        name=name,
                        tenant_id=collection.tenant_id,
                        status=StatusEnum.VALID.value,
                    )
                kwargs["name"] = name

            kwargs["update_time"] = current_timestamp()
            return cls.model.update(**kwargs).where(cls.model.id == collection_id).execute() > 0
        except Exception as e:
            logging.error(f"Error updating collection {collection_id}: {e}")
            return False

    @classmethod
    @DB.connection_context()
    def delete_collection(cls, collection_id: str) -> bool:
        """Delete collection"""
        try:
            with DB.atomic():
                run_ids = [
                    r.id for r in EvaluationRun.select(EvaluationRun.id).where(
                        EvaluationRun.collection_id == collection_id
                    )
                ]
                if run_ids:
                    EvaluationResult.delete().where(
                        EvaluationResult.run_id.in_(run_ids)
                    ).execute()
                    EvaluationRun.delete().where(
                        EvaluationRun.id.in_(run_ids)
                    ).execute()
                EvaluationCase.delete().where(
                    EvaluationCase.collection_id == collection_id
                ).execute()
                return cls.model.delete().where(cls.model.id == collection_id).execute() > 0
        except Exception as e:
            logging.error(f"Error deleting collection {collection_id}: {e}")
            return False

    # ==================== Test Case Management ====================

    @classmethod
    @DB.connection_context()
    def add_test_case(cls, collection_id: str, variable: Dict[str, Any],
                     relevant_doc_ids: Optional[List[str]] = None,
                     relevant_kb_ids: Optional[List[str]] = None,
                     metadata: Optional[Dict[str, Any]] = None) -> Tuple[bool, str]:
        """
        Add a test case to a collection.

        Args:
            collection_id: Collection ID
            question: Test question
            reference_answer: Optional ground truth answer
            relevant_doc_ids: Optional list of relevant document IDs
            relevant_kb_ids: Optional list of relevant knowledge base IDs
            metadata: Optional additional metadata

        Returns:
            (success, case_id or error_message)
        """
        try:
            case_id = get_uuid()
            case = {
                "id": case_id,
                "collection_id": collection_id,
                "variable": variable,
                "relevant_doc_ids": relevant_doc_ids,
                "relevant_kb_ids": relevant_kb_ids,
                "metadata": metadata,
                "create_time": current_timestamp()
            }

            if not EvaluationCase.create(**case):
                return False, "Failed to create test case"

            return True, case_id
        except Exception as e:
            logging.error(f"Error adding test case: {e}")
            return False, str(e)

    @classmethod
    @DB.connection_context()
    def get_test_cases_count(cls, run_id: str) -> int:
        try:
            if not run_id:
                return 0
            run = EvaluationRun.get_or_none(EvaluationRun.id == run_id)
            if not run:
                return 0
            cnt = (
                EvaluationCase.select(fn.COUNT(EvaluationCase.id))
                .where(EvaluationCase.collection_id == run.collection_id)
                .scalar()
            )
            return int(cnt or 0)
        except Exception as e:
            logging.error(f"Error getting test cases count for run {run_id}: {e}")
            return 0

    @classmethod
    @DB.connection_context()
    def get_test_cases(cls, collection_id: str) -> List[Dict[str, Any]]:
        """Get all test cases for a collection"""
        try:
            cases = EvaluationCase.select().where(
                EvaluationCase.collection_id == collection_id
            ).order_by(EvaluationCase.create_time)

            return [c.to_dict() for c in cases]
        except Exception as e:
            logging.error(f"Error getting test cases for collection {collection_id}: {e}")
            return []

    @classmethod
    @DB.connection_context()
    def list_test_cases(
        cls, collection_id: str, page: int = 1, page_size: int = 20
    ) -> Dict[str, Any]:
        """List test cases for a collection with pagination."""
        try:
            query = EvaluationCase.select().where(
                EvaluationCase.collection_id == collection_id
            ).order_by(EvaluationCase.create_time)

            total = query.count()
            cases = query.paginate(page, page_size)
            return {"total": total, "cases": [c.to_dict() for c in cases]}
        except Exception as e:
            logging.error(f"Error listing test cases for collection {collection_id}: {e}")
            return {"total": 0, "cases": []}

    @classmethod
    @DB.connection_context()
    def delete_test_case(cls, case_id: str) -> bool:
        """Delete a test case"""
        try:
            with DB.atomic():
                EvaluationResult.delete().where(
                    EvaluationResult.case_id == case_id
                ).execute()
                return EvaluationCase.delete().where(
                    EvaluationCase.id == case_id
                ).execute() > 0
        except Exception as e:
            logging.error(f"Error deleting test case {case_id}: {e}")
            return False

    @classmethod
    @DB.connection_context()
    def update_test_case(cls, case_id: str, **kwargs) -> bool:
        """Update a test case by ID"""
        try:
            allowed_keys = {"variable", "relevant_doc_ids", "relevant_kb_ids", "metadata"}
            data = {k: v for k, v in kwargs.items() if k in allowed_keys}
            if not data:
                return False
            with DB.atomic():
                updated = EvaluationCase.update(**data).where(
                    EvaluationCase.id == case_id
                ).execute() > 0
                if not updated:
                    return False
                EvaluationResult.delete().where(
                    EvaluationResult.case_id == case_id
                ).execute()
                return True
        except Exception as e:
            logging.error(f"Error updating test case {case_id}: {e}")
            return False

    @classmethod
    @DB.connection_context()
    def import_test_cases(cls, collection_id: str, cases: List[Dict[str, Any]]) -> Tuple[int, int]:
        """
        Bulk import test cases from a list.

        Args:
            collection_id: Collection ID
            cases: List of test case dictionaries

        Returns:
            (success_count, failure_count)
        """
        success_count = 0
        failure_count = 0
        case_instances = []

        if not cases:
            return success_count, failure_count

        cur_timestamp = current_timestamp()

        try:
            for case_data in cases:
                variable = case_data.get("variable",None)
                if not isinstance(variable, dict) or not variable:
                    if case_data.get("question", ""):
                        variable = {
                            "question": case_data.get("question", "").strip(),
                            "reference_answer": case_data.get("reference_answer", "").strip()
                        }
                    else:
                        failure_count += 1
                        continue
                case_id = get_uuid()
                case_info = {
                    "id": case_id,
                    "collection_id": collection_id,
                    "variable": variable,
                    "relevant_doc_ids": case_data.get("relevant_doc_ids"),
                    "relevant_kb_ids": case_data.get("relevant_kb_ids"),
                    "metadata": case_data.get("metadata"),
                    "create_time": cur_timestamp
                }

                case_instances.append(case_info)
            if case_instances:
                bulk_insert_into_db(
                    model=EvaluationCase,
                    data_source=case_instances,
                    replace_on_conflict=True,
                )
            success_count = len(case_instances)

        except Exception as e:
            logging.error(f"Error bulk importing test cases: {str(e)}")
            failure_count = len(cases)
            success_count = 0

        return success_count, failure_count

    # ==================== Evaluation Run Management ====================

    @classmethod
    @DB.connection_context()
    def create_run_config(cls, collection_id: str, target_type: str, target_id: str,
                          user_id: str, name: Optional[str] = None,
                          config_snapshot: Optional[Dict[str, Any]] = {},
                          run_id: str = None
                          ) -> Tuple[bool, str]:
        """
        Create an evaluation run config without execution.

        Args:
            collection_id: Collection ID
            target_type: Target type to evaluate
            target_id: Target ID to evaluate
            user_id: User ID who creates the run
            name: Optional run name
            config_snapshot: Optional config snapshot to store

        Returns:
            (success, run_id or error_message)
        """
        try:
            if not config_snapshot.get("target"):
                if target_type == "chat":
                    success, dialog = DialogService.get_by_id(target_id)
                    if not success:
                        return False, "Dialog not found"
                    config_snapshot["target"] = dialog.to_dict()
                    if not name:
                        target_name = dialog.name
            success, ten = TenantService.get_by_id(user_id)
            config_snapshot["metrics"] = config_snapshot.get("metrics")
            if not config_snapshot["metrics"]:
                config_snapshot["metrics"] = {
                "context_relevance": {"enable": True, "llm_id": ten.llm_id}, 
                "faithfulness": {"enable": True, "llm_id": ten.llm_id},
                "semantic_similarity":{"enable": True, "llm_id": ten.llm_id}
                }
            else:
                for metric_config in config_snapshot["metrics"].values():
                    if isinstance(metric_config, dict) and not metric_config.get("llm_id"):
                        metric_config["llm_id"] = ten.llm_id

            if not name:
                target_name = dialog.name if dialog else "target"
                version = cls._next_run_version(collection_id, target_type, target_id, target_name)
                name = f"{target_name}_v{version}"
            else:
                name = name.strip()
                name = duplicate_name(
                    EvaluationRun.query,
                    name=name,
                    collection_id=collection_id,
                    target_type=target_type,
                    target_id=target_id,
                )

            run = {
                "collection_id": collection_id,
                "target_type": target_type,
                "target_id": target_id,
                "name": name,
                "config_snapshot": config_snapshot,
                "metrics_summary": None,
                "status": EvaluationRunStatus.PENDING,
                "created_by": user_id,
                "create_time": current_timestamp(),
                "complete_time": None
            }

            if run_id:
                EvaluationRun.update(**run).where(EvaluationRun.id == run_id).execute()
            else:
                run["id"] = get_uuid()
                run_id = run["id"]
                if not EvaluationRun.create(**run):
                    return False, "Failed to create evaluation run"

            return True, run_id
        except Exception as e:
            logging.error(f"Error creating evaluation run: {e}")
            return False, str(e)

    @classmethod
    @DB.connection_context()
    def get_run(cls, run_id: str) -> Optional[Dict[str, Any]]:
        """Get a run by ID"""
        try:
            run = EvaluationRun.get_by_id(run_id)
            return run.to_dict() if run else None
        except Exception as e:
            logging.error(f"Error getting run {run_id}: {e}")
            return None

    @classmethod
    @DB.connection_context()
    def list_runs(cls, target_id: str, page: int, page_size: int, 
                  keywords: Optional[str] = None) -> List[Dict[str, Any]]:
        """List runs with optional filters (no pagination)"""
        try:
            query = EvaluationRun.select().where(EvaluationRun.target_id == target_id)
            if keywords:
                query = query.where(EvaluationRun.name.contains(keywords.lower()))

            total = query.count()
            query = query.order_by(EvaluationRun.create_time.desc()).paginate(page, page_size)
            return { "total": total, "runs": [r.to_dict() for r in query] }
        except Exception as e:
            logging.error(f"Error listing runs: {e}")
        return {}

    @classmethod
    @DB.connection_context()
    def update_run(cls, run_id: str, **kwargs) -> bool:
        """Update a run's name or config_snapshot"""
        try:
            run = EvaluationRun.get_by_id(run_id)
            if not run:
                return False

            update_data = {}
            if "name" in kwargs and isinstance(kwargs["name"], str):
                name = kwargs["name"].strip()
                if name and name != run.name:
                    name = duplicate_name(
                        EvaluationRun.query,
                        name=name,
                        collection_id=run.collection_id,
                        target_type=run.target_type,
                        target_id=run.target_id,
                    )
                update_data["name"] = name

            if "config_snapshot" in kwargs:
                update_data["config_snapshot"] = kwargs["config_snapshot"]

            if not update_data:
                return False

            return EvaluationRun.update(**update_data).where(
                EvaluationRun.id == run_id
            ).execute() > 0
        except Exception as e:
            logging.error(f"Error updating run {run_id}: {e}")
            return False

    @classmethod
    @DB.connection_context()
    def duplicate_run(cls, run_id: str, user_id: str,
                      name: Optional[str] = None) -> Tuple[bool, str]:
        """Duplicate a run config"""
        try:
            run = EvaluationRun.get_by_id(run_id)
            if not run:
                return False, "Run not found"

            if not name:
                base_name = run.target_id
                if run.target_type == "dialog":
                    success, dialog = DialogService.get_by_id(run.target_id)
                    if success:
                        base_name = dialog.name
                version = cls._next_run_version(run.collection_id, run.target_type, run.target_id, base_name)
                name = f"{base_name}_v{version}"
            else:
                name = name.strip()
                name = duplicate_name(
                    EvaluationRun.query,
                    name=name,
                    collection_id=run.collection_id,
                    target_type=run.target_type,
                    target_id=run.target_id,
                )

            new_run_id = get_uuid()
            new_run = {
                "id": new_run_id,
                "collection_id": run.collection_id,
                "target_type": run.target_type,
                "target_id": run.target_id,
                "name": name,
                "config_snapshot": run.config_snapshot,
                "metrics_summary": None,
                "status": EvaluationRunStatus.PENDING,
                "created_by": user_id,
                "create_time": current_timestamp(),
                "complete_time": None
            }

            if not EvaluationRun.create(**new_run):
                return False, "Failed to duplicate evaluation run"

            return True, new_run_id
        except Exception as e:
            logging.error(f"Error duplicating run {run_id}: {e}")
            return False, str(e)

    @classmethod
    @DB.connection_context()
    def delete_run(cls, run_id: str) -> bool:
        """Delete a run and its results"""
        try:
            with DB.atomic():
                EvaluationResult.delete().where(
                    EvaluationResult.run_id == run_id
                ).execute()
                return EvaluationRun.delete().where(
                    EvaluationRun.id == run_id
                ).execute() > 0
        except Exception as e:
            logging.error(f"Error deleting run {run_id}: {e}")
            return False

    @classmethod
    @DB.connection_context()
    def queue_run_task(
        cls,
        run_id,
        priority: int = 0,
        case_ids: Optional[str] = None,
        metrics_name: Optional[List[str]] = None,
    ) -> Tuple[bool, str]:
        doc = DocumentService.one()
        if not doc:
            return False, None
        run = EvaluationRun.get_by_id(run_id)
        if not run:
            return False, "Evaluation run not found"
        collection = EvaluationCollection.get_by_id(run.collection_id)
        if not collection:
            return False, "Evaluation collection not found"
        normalized_case_ids = cls._normalize_case_ids(case_ids)
        normalized_metrics = cls._normalize_metrics(metrics_name)
        task_id = queue_reembedding_dup_tasks(
            doc["id"],
            "evaluation",
            priority,
            eva_run_id=run_id,
            case_ids=normalized_case_ids,
            metrics_name=normalized_metrics,
            tenant_id=collection.tenant_id,
        )
        EvaluationRun.update(
                task_id=task_id,
                status=EvaluationRunStatus.PENDING
            ).where(EvaluationRun.id == run_id).execute()
        return True, task_id
    
    @classmethod
    @DB.connection_context()
    def cancel_run_task(cls, run_id) -> Tuple[bool, str]:
        EvaluationRun.update(
                status=EvaluationRunStatus.CANCEL
            ).where(EvaluationRun.id == run_id).execute()

    # ==================== Evaluation Execution ====================

    @classmethod
    @DB.connection_context()
    def execute_run_case(cls, run_id: str, case_id: str, user_id: str) -> bool:
        """Execute a single case within a run"""
        try:
            run = EvaluationRun.get_by_id(run_id)
            if not run:
                return False
            if run.target_type != "dialog":
                return False
            ok, dialog = DialogService.get_by_id(run.target_id)
            if not ok:
                return False

            case = EvaluationCase.get_or_none(
                (EvaluationCase.id == case_id) &
                (EvaluationCase.collection_id == run.collection_id)
            )
            if not case:
                return False

            EvaluationRun.update(status=EvaluationRunStatus.RUNNING).where(EvaluationRun.id == run_id).execute()
            variable = case.variable or {}
            question = variable.get("question", "")
            messages = [{"role": "user", "content": question}]
            answer, retrieved_chunks, execution_time = cls._execute_target(dialog, messages)
            result = cls._save_execution_result(run_id, case.id, answer, retrieved_chunks, execution_time)
            if not result:
                return False

            EvaluationRun.update(
                status=EvaluationRunStatus.COMPLETED,
                complete_time=current_timestamp()
            ).where(EvaluationRun.id == run_id).execute()

            return True
        except Exception as e:
            logging.error(f"Error executing run case {run_id}/{case_id}: {e}")
            return False
        
    @classmethod
    @DB.connection_context()
    def _check_run(cls, run_id: str):
        run = EvaluationRun.get_by_id(run_id)
        if not run:
            raise LookupError("The evaluation run is missing.")
        if run.target_type != "chat":
            raise TypeError("Only for chat.")
        ok, dialog = DialogService.get_by_id(run.target_id)
        if not ok:
            raise LookupError("The chat is missing.")

        test_cases = cls.get_test_cases(run.collection_id)
        if not test_cases:
            raise LookupError("The test case is missing.")
        
        return dialog, test_cases

    @staticmethod
    def _normalize_case_ids(value: Optional[Union[str, Sequence[str]]]) -> Optional[List[str]]:
        if not value:
            return None
        if isinstance(value, str):
            candidates = [v.strip() for v in value.split(",")]
        else:
            candidates = [str(v).strip() for v in value]
        normalized = [v for v in candidates if v]
        return normalized or None

    @staticmethod
    def _normalize_metrics(value: Optional[Union[str, Sequence[str]]]) -> Optional[List[str]]:
        if not value:
            return None
        if isinstance(value, str):
            candidates = [v.strip() for v in value.split(",")]
        else:
            candidates = []
            for v in value:
                if isinstance(v, EvaluationMetric):
                    candidates.append(v.value)
                else:
                    candidates.append(str(v).strip())
        normalized = []
        for v in candidates:
            if not v:
                continue
            try:
                normalized.append(EvaluationMetric(v).value)
            except ValueError:
                logging.warning("Ignoring unknown evaluation metric: %s", v)
        return normalized or None

    @classmethod
    async def execute_run_all_cases(
        cls,
        run_id: str,
        callback=None,
        case_ids: Optional[Union[str, Sequence[str]]] = None,
        metrics_name: Optional[List[str]] = None,
        max_concurrency: Optional[int] = None,
    ) -> bool:
        """Execute all (or filtered) cases within a run (async + parallel)."""
        normalized_metrics = cls._normalize_metrics(metrics_name)
        normalized_case_ids = cls._normalize_case_ids(case_ids)

        try:
            with DB.connection_context():
                run = EvaluationRun.get_by_id(run_id)
                if not run:
                    raise LookupError("The evaluation run is missing.")
                if run.target_type != "chat":
                    raise TypeError("Only for chat.")

                target_id = run.target_id
                ok, _ = DialogService.get_by_id(target_id)
                if not ok:
                    raise LookupError("The chat is missing.")

                test_cases = cls.get_test_cases(run.collection_id)
                if not test_cases:
                    raise LookupError("The test case is missing.")

                if normalized_case_ids:
                    case_map = {c["id"]: c for c in test_cases}
                    filtered = []
                    missing = []
                    for cid in normalized_case_ids:
                        case = case_map.get(cid)
                        if case:
                            filtered.append(case)
                        else:
                            missing.append(cid)
                    if missing:
                        logging.warning("Some requested case_ids not found for run %s: %s", run_id, missing)
                    if not filtered:
                        raise LookupError("No matching test cases found for provided case_ids.")
                    test_cases = filtered

                EvaluationRun.update(status=EvaluationRunStatus.RUNNING).where(EvaluationRun.id == run_id).execute()
                total = len(test_cases)
                if callback:
                    callback(prog=0.0, msg=f"Evaluation run started: {total} cases.")

                for ca in test_cases:
                    cls._save_execution_result(run_id, ca["id"], "", [], -1)

            if total == 0:
                raise LookupError("No test cases remain after filtering.")

            if max_concurrency is None:
                try:
                    max_concurrency = int(os.getenv("RAGFLOW_EVALUATION_MAX_CONCURRENCY", "4"))
                except Exception:
                    max_concurrency = 4
            max_concurrency = max(1, int(max_concurrency))

            sem = asyncio.Semaphore(max_concurrency)
            done_count = 0
            done_lock = asyncio.Lock()

            async def run_one(ca: Dict[str, Any]):
                nonlocal done_count

                async with sem:
                    def work():
                        with DB.connection_context():
                            ok2, dialog2 = DialogService.get_by_id(target_id)
                            if not ok2:
                                raise LookupError("The chat is missing.")
                            try:
                                for k,v in run.config_snapshot.get("target", {}).items():
                                    if hasattr(Dialog, k):
                                        setattr(dialog2, k, v)
                            except Exception as e:
                                logging.exception(e)
                            variable = ca.get("variable", {}) or {}
                            question = variable.get("question", "")
                            messages = [{"role": "user", "content": question}]
                            answer, retrieved_chunks, execution_time = cls._execute_target(dialog2, messages)
                            cls._save_execution_result(run_id, ca["id"], answer, retrieved_chunks, execution_time)
                            if not cls.run_metrics_for_case(
                                run_id,
                                ca["id"],
                                dialog2.tenant_id,
                                normalized_metrics,
                            ):
                                raise RuntimeError(f"Failed to compute metrics for case {ca['id']}.")

                    await asyncio.to_thread(work)

                async with done_lock:
                    done_count += 1
                    if callback:
                        callback(prog=done_count / total)

            tasks = [asyncio.create_task(run_one(ca)) for ca in test_cases]
            try:
                await asyncio.gather(*tasks, return_exceptions=False)
            except Exception:
                for t in tasks:
                    t.cancel()
                await asyncio.gather(*tasks, return_exceptions=True)
                raise

            cls.recompute_metrics_summary(run_id)
            with DB.connection_context():
                EvaluationRun.update(
                    status=EvaluationRunStatus.COMPLETED,
                    complete_time=current_timestamp()
                ).where(EvaluationRun.id == run_id).execute()
            if callback:
                callback(prog=1, msg="Done")
            return True
        except Exception as e:
            logging.error(f"Error executing run {run_id}: {e}")
            with DB.connection_context():
                EvaluationRun.update(
                    status=EvaluationRunStatus.FAILED,
                    complete_time=current_timestamp()
                ).where(EvaluationRun.id == run_id).execute()
            if callback:
                callback(prog=-1, msg=str(e))
            return False

    @classmethod
    @DB.connection_context()
    def run_metrics_for_case(cls, run_id: str, case_id: str, user_id: str, metric_names: list|None=None) -> bool:
        """Compute all metrics for a case"""        
        metric_names = cls._normalize_metrics(metric_names)
        if not metric_names:
            metric_names = [m.value for m in DEFAULT_EVALUATION_METRICS]
        try:
            run = EvaluationRun.get_by_id(run_id)
            if not run:
                raise LookupError("The evaluation run is missing.")
            
            case = EvaluationCase.get_or_none(
                (EvaluationCase.id == case_id) &
                (EvaluationCase.collection_id == run.collection_id)
            )
            if not case:
                raise LookupError("The test case is missing.")
            
            result = EvaluationResult.get_or_none(
                (EvaluationResult.run_id == run_id) &
                (EvaluationResult.case_id == case_id)
            )
            if not result:
                raise LookupError("The test case result is missing.")

            metrics = dict(result.metrics or {})
            computed_any = False
            variable = case.variable or {}
            for met in metric_names:
                metric_config = run.config_snapshot.get("metrics", {}).get(met)
                if not metric_config or not metric_config.get("enable", True):
                    continue
                _metrics = cls._compute_metrics(
                    question=variable.get("question", ""),
                    generated_answer=result.generated_answer,
                    reference_answer=variable.get("reference_answer"),
                    retrieved_chunks=result.retrieved_chunks or {},
                    relevant_doc_ids=case.relevant_doc_ids,
                    llm_id=metric_config["llm_id"],
                    user_id=user_id,
                    metrics_type=met
                )
                if _metrics:
                    computed_any = True
                metrics.update(_metrics)
            if not computed_any:
                return False
            updated = EvaluationResult.update(metrics=metrics).where(
                EvaluationResult.id == result.id
            ).execute() > 0
            return updated
        except Exception as e:
            logging.error(f"Error computing metrics for {run_id}/{case_id}: {e}")
            return False

    @classmethod
    @DB.connection_context()
    def run_metrics_for_cases(cls, run_id: str,
                              user_id: str, 
                              metrics: list|None= None) -> Tuple[int, int]:
        """Compute all metrics for all cases in a run"""
        metric_names = cls._normalize_metrics(metrics)
        if not metric_names:
            metric_names = [m.value for m in DEFAULT_EVALUATION_METRICS]
        success_count = 0
        failure_count = 0
        run = EvaluationRun.get_by_id(run_id)
        if not run:
            return success_count, failure_count
        cases = cls.get_test_cases(run.collection_id)
        for case in cases:
            if cls.run_metrics_for_case(run_id, case["id"], user_id, metric_names):
                success_count += 1
            else:
                failure_count += 1
        if success_count:
            cls.recompute_metrics_summary(run_id)
        return success_count, failure_count

    @classmethod
    @DB.connection_context()
    def run_metric_for_cases(cls, run_id: str, metric_name: str, user_id: str) -> Tuple[int, int]:
        """Compute a single metric for all cases in a run."""
        if not metric_name:
            return 0, 0
        return cls.run_metrics_for_cases(run_id, user_id, [metric_name])

    @classmethod
    @DB.connection_context()
    def recompute_metrics_summary(cls, run_id: str) -> bool:
        """Recompute summary metrics for a run"""
        try:
            results = EvaluationResult.select().where(
                EvaluationResult.run_id == run_id
            )
            metrics_summary = cls._compute_summary_metrics([r.to_dict() for r in results])
            return EvaluationRun.update(
                metrics_summary=metrics_summary
            ).where(EvaluationRun.id == run_id).execute() > 0
        except Exception as e:
            logging.error(f"Error recomputing metrics summary for {run_id}: {e}")
            return False

    @classmethod
    @DB.connection_context()
    def clear_result(cls, run_id: str, case_id: str) -> bool:
        """Clear result content without deleting the record"""
        try:
            result = EvaluationResult.get_or_none(
                (EvaluationResult.run_id == run_id) &
                (EvaluationResult.case_id == case_id)
            )
            if not result:
                return False
            return EvaluationResult.update(
                generated_answer="",
                retrieved_chunks={"chunks": [], "doc_aggs":[]},
                metrics={},
                execution_time=0.0,
                token_usage=None,
            ).where(EvaluationResult.id == result.id).execute() > 0
        except Exception as e:
            logging.error(f"Error clearing result for {run_id}/{case_id}: {e}")
            return False

    @classmethod
    @DB.connection_context()
    def clear_result_metric(cls, run_id: str, case_id: str, metric_name: str) -> bool:
        """Remove a single metric from result metrics"""
        try:
            result = EvaluationResult.get_or_none(
                (EvaluationResult.run_id == run_id) &
                (EvaluationResult.case_id == case_id)
            )
            if not result:
                return False
            metrics = result.metrics or {}
            if metric_name not in metrics:
                return False
            metrics.pop(metric_name, None)
            updated = EvaluationResult.update(metrics=metrics).where(
                EvaluationResult.id == result.id
            ).execute() > 0
            if updated:
                cls.recompute_metrics_summary(run_id)
            return updated
        except Exception as e:
            logging.error(f"Error clearing metric {metric_name} for {run_id}/{case_id}: {e}")
            return False

    @classmethod
    @DB.connection_context()
    def clear_result_generated_answer(cls, run_id: str, case_id: str) -> bool:
        """Clear generated answer content"""
        try:
            result = EvaluationResult.get_or_none(
                (EvaluationResult.run_id == run_id) &
                (EvaluationResult.case_id == case_id)
            )
            if not result:
                return False
            return EvaluationResult.update(
                generated_answer=""
            ).where(EvaluationResult.id == result.id).execute() > 0
        except Exception as e:
            logging.error(f"Error clearing generated answer for {run_id}/{case_id}: {e}")
            return False

    @staticmethod
    @DB.connection_context()
    def _execute_target(dialog: Any, messages: List[Dict[str, Any]]) -> Tuple[str, List[Dict[str, Any]], float]:
        """Execute target dialog and return answer, retrieved chunks, and execution time."""
        start_time = timer()
        answer = ""
        retrieved_chunks = {}

        def _sync_from_async_gen(async_gen):
            result_queue: queue.Queue = queue.Queue()

            def runner():
                loop = asyncio.new_event_loop()
                asyncio.set_event_loop(loop)

                async def consume():
                    try:
                        async for item in async_gen:
                            result_queue.put(item)
                    except Exception as e:
                        result_queue.put(e)
                    finally:
                        result_queue.put(StopIteration)

                loop.run_until_complete(consume())
                loop.close()

            threading.Thread(target=runner, daemon=True).start()

            while True:
                item = result_queue.get()
                if item is StopIteration:
                    break
                if isinstance(item, Exception):
                    raise item
                yield item

        def chat(dialog, messages, stream=True, **kwargs):
            from api.db.services.dialog_service import async_chat

            return _sync_from_async_gen(async_chat(dialog, messages, stream=stream, **kwargs))

        stru_ans = {}
        for ans in chat(dialog, messages, stream=False):
            stru_ans = ans
        answer = stru_ans.get("answer", "")
        retrieved_chunks = stru_ans.get("reference", {})
        execution_time = timer() - start_time
        return answer, retrieved_chunks, execution_time

    @classmethod
    @DB.connection_context()
    def _save_execution_result(cls, run_id: str, case_id: str, answer: str,
                               retrieved_chunks: Dict[str, Any],
                               execution_time: float) -> Dict[str, Any]:
        existing = EvaluationResult.get_or_none(
            (EvaluationResult.run_id == run_id) &
            (EvaluationResult.case_id == case_id)
        )
        if existing:
            update_data = {
                "generated_answer": answer,
                "retrieved_chunks": retrieved_chunks,
                "metrics": {},
                "execution_time": execution_time,
                "token_usage": None,
            }
            EvaluationResult.update(**update_data).where(
                EvaluationResult.id == existing.id
            ).execute()
            return {
                "id": existing.id,
                "run_id": run_id,
                "case_id": case_id,
                "generated_answer": answer,
                "retrieved_chunks": retrieved_chunks,
                "metrics": {},
                "execution_time": execution_time,
                "token_usage": None,
            }

        result_id = get_uuid()
        result = {
            "id": result_id,
            "run_id": run_id,
            "case_id": case_id,
            "generated_answer": answer,
            "retrieved_chunks": retrieved_chunks,
            "metrics": {},
            "execution_time": execution_time,
            "token_usage": None,  # TODO: Track token usage
            "create_time": current_timestamp()
        }

        EvaluationResult.create(**result)
        return result

    @classmethod
    def _compute_metrics(cls, question: str, generated_answer: str,
                        reference_answer: Optional[str],
                        retrieved_chunks: Dict[str, Any],
                        relevant_doc_ids: Optional[List[str]],
                        llm_id: str,
                        user_id: str,
                        metrics_type: str) -> Dict[str, float]:
        """
        Compute evaluation metrics for a single test case.

        Returns:
            Dictionary of metric names to values
        """
        metrics = {}

        # Retrieval metrics (if ground truth docs provided)
        if relevant_doc_ids:
            retrieved_doc_ids = [c.get("doc_id") for c in retrieved_chunks if c.get("doc_id")]
            metrics.update(cls._compute_retrieval_metrics(retrieved_doc_ids, relevant_doc_ids))

        # Generation metrics
        if generated_answer:
            # Basic metrics
            metrics["answer_length"] = len(generated_answer)
            metrics["has_answer"] = 1.0 if generated_answer.strip() else 0.0
            if reference_answer:
                metrics["blue_score"] = cls._compute_blue_score(generated_answer, reference_answer)

            metrics.update(
                cls._compute_llm_metrics(
                    question=question,
                    generated_answer=generated_answer,
                    reference_answer=reference_answer,
                    retrieved_chunks=retrieved_chunks,
                    llm_id=llm_id,
                    user_id=user_id,
                    metrics_type=metrics_type,
                )
            )

        return metrics

    @classmethod
    def _compute_llm_metrics(cls, question: str, generated_answer: str,
                             reference_answer: Optional[str],
                             retrieved_chunks: Dict[str, Any],
                             llm_id: str, 
                             user_id: str,
                             metrics_type: str) -> Dict[str, float]:
        if not question or not generated_answer:
            return {}

        context = cls._build_context(retrieved_chunks)
        reference = reference_answer or ""

        llm_type = cls._llm_type_from_llm_id(user_id, llm_id)
        try:
            from rag.prompts.generator import rag_judge_metrics

            model_config = get_model_config_from_provider_instance(user_id, llm_type, llm_id or None)
            llm = LLMBundle(user_id, model_config)
            judge_result = llm._run_coroutine_sync(
                rag_judge_metrics(
                    llm,
                    question=cls._truncate_text(question, 256),
                    answer=cls._truncate_text(generated_answer, 512),
                    context=context,
                    reference_answer=cls._truncate_text(reference, 512) if reference else "",
                    metric_names=[metrics_type] if metrics_type else None,
                )
            )
        except Exception as e:
            logging.error(f"LLM judge failed: {e}")
            return {}

        return cls._normalize_judge_metrics(judge_result, reference_answer)

    @staticmethod
    def _normalize_judge_metrics(result: Any, reference_answer: Optional[str]) -> Dict[str, float]:
        if not isinstance(result, dict):
            return {}
        key, val = list(result.items())[0]
        key = key.value if isinstance(key, EvaluationMetric) else str(key)
        def _get_score(value: Any) -> Optional[float]:
            try:
                score_f = float(value)
            except (TypeError, ValueError):
                return None
            if score_f < 0.0:
                score_f = 0.0
            elif score_f > 1.0:
                score_f = 1.0
            return score_f
        return {key: _get_score(val.get("score")), f"{key}_reason": val.get("reason", "")}

    @staticmethod
    def _llm_type_from_llm_id(tenant_id, llm_id: str) -> LLMType:
        try:
            llm_type = get_model_type_by_name(tenant_id, llm_id)
            if "image2text" in llm_type:
                return LLMType.IMAGE2TEXT
        except Exception:
            pass
        return LLMType.CHAT

    @staticmethod
    def _truncate_text(text: str, max_chars: int) -> str:
        if not text:
            return ""
        text = text.strip()
        if len(text) > max_chars:
            return text[:max_chars]
        return text

    @staticmethod
    def _tokenize_text(text: str) -> List[str]:
        if not text:
            return []
        text = text.lower()
        normalized = "".join(ch if ch.isalnum() else " " for ch in text)
        return [tok for tok in normalized.split() if tok]

    @classmethod
    def _compute_blue_score(cls, candidate: str, reference: str) -> float:
        cand_tokens = cls._tokenize_text(candidate)
        ref_tokens = cls._tokenize_text(reference)
        if not cand_tokens or not ref_tokens:
            return 0.0

        ref_counts = {}
        for tok in ref_tokens:
            ref_counts[tok] = ref_counts.get(tok, 0) + 1

        cand_counts = {}
        for tok in cand_tokens:
            cand_counts[tok] = cand_counts.get(tok, 0) + 1

        overlap = 0
        for tok, count in cand_counts.items():
            overlap += min(count, ref_counts.get(tok, 0))

        precision = overlap / len(cand_tokens) if cand_tokens else 0.0
        c_len = len(cand_tokens)
        r_len = len(ref_tokens)
        if c_len > r_len:
            bp = 1.0
        else:
            bp = math.exp(1.0 - (r_len / c_len)) if c_len else 0.0

        return bp * precision

    @classmethod
    def _build_context(cls, retrieved_chunks: Dict[str, Any],
                       max_chars: int = 8192, max_chunks: int = 6) -> str:
        retrieved_chunks = retrieved_chunks.get("chunks", [])
        if not retrieved_chunks:
            return ""
        parts = []
        total = 0
        for chunk in retrieved_chunks[:max_chunks]:
            content = (
                chunk.get("content")
                or chunk.get("content_with_weight")
                or remove_redundant_spaces(chunk.get("content_ltks"))
                or ""
            )
            if not content:
                continue
            content = str(content).strip()
            if not content:
                continue
            content = cls._truncate_text(content, 1024)
            remaining = max_chars - total
            if remaining <= 0:
                break
            if len(content) > remaining:
                content = content[:remaining]
            parts.append(content)
            total += len(content)
            if total >= max_chars:
                break
        return "\n\n".join(parts)

    @classmethod
    def _compute_retrieval_metrics(cls, retrieved_ids: List[str],
                                   relevant_ids: List[str]) -> Dict[str, float]:
        """
        Compute retrieval metrics.

        Args:
            retrieved_ids: List of retrieved chunk IDs
            relevant_ids: List of relevant chunk IDs (ground truth)

        Returns:
            Dictionary of retrieval metrics
        """
        if not relevant_ids:
            return {}

        retrieved_set = set(retrieved_ids)
        relevant_set = set(relevant_ids)

        # Precision: proportion of retrieved that are relevant
        precision = len(retrieved_set & relevant_set) / len(retrieved_set) if retrieved_set else 0.0

        # Recall: proportion of relevant that were retrieved
        recall = len(retrieved_set & relevant_set) / len(relevant_set) if relevant_set else 0.0

        # F1 score
        f1 = 2 * (precision * recall) / (precision + recall) if (precision + recall) > 0 else 0.0

        # Hit rate: whether any relevant chunk was retrieved
        hit_rate = 1.0 if (retrieved_set & relevant_set) else 0.0

        # MRR (Mean Reciprocal Rank): position of first relevant chunk
        mrr = 0.0
        for i, chunk_id in enumerate(retrieved_ids, 1):
            if chunk_id in relevant_set:
                mrr = 1.0 / i
                break

        return {
            "precision": precision,
            "recall": recall,
            "f1_score": f1,
            "hit_rate": hit_rate,
            "mrr": mrr
        }

    @classmethod
    def _compute_summary_metrics(cls, results: List[Dict[str, Any]]) -> Dict[str, Any]:
        """
        Compute summary metrics across all test cases.

        Format:
        {
            "metric_name": {
                "type": "",
                "config": "",
                "summary": <value>
            }
        }
        """
        if not results:
            return {}

        metric_sums = {}
        metric_counts = {}

        for result in results:
            metrics = result.get("metrics", {})
            for key, value in metrics.items():
                if isinstance(value, (int, float)):
                    metric_sums[key] = metric_sums.get(key, 0) + value
                    metric_counts[key] = metric_counts.get(key, 0) + 1

            exec_time = result.get("execution_time")
            if isinstance(exec_time, (int, float)):
                metric_sums["execution_time"] = metric_sums.get("execution_time", 0) + exec_time
                metric_counts["execution_time"] = metric_counts.get("execution_time", 0) + 1

        summary = {}
        for key, total in metric_sums.items():
            count = metric_counts.get(key, 0)
            if count:
                summary[key] = {
                    "type": "",
                    "config": "",
                    "summary": total / count
                }

        return summary

    # ==================== Results & Analysis ====================

    @classmethod
    @DB.connection_context()
    def get_run_results(cls, run_id: str, page: int, page_size: int) -> Dict[str, Any]:
        """Get results for an evaluation run"""
        try:
            run = EvaluationRun.get_by_id(run_id)
            if not run:
                return {}

            fields = [
                EvaluationResult.id,
                EvaluationResult.case_id,
                EvaluationCase.variable,
                EvaluationResult.execution_time,
                EvaluationResult.token_usage,
                EvaluationResult.generated_answer,
                EvaluationResult.retrieved_chunks,
                EvaluationResult.metrics,
            ]
            results = EvaluationResult.select(*fields)\
                .join(EvaluationCase, on=(EvaluationResult.case_id == EvaluationCase.id)).where(
                EvaluationResult.run_id == run_id
            ).order_by(EvaluationResult.id)
            total = results.count()
            results = results.paginate(page, page_size).dicts()

            return {
                "run": run.to_dict(),
                "results": list(results),
                "total": total
            }
        except Exception as e:
            logging.error(f"Error getting run results {run_id}: {e}")
            return {}
