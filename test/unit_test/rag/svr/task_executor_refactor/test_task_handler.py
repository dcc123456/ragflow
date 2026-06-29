#
#  Copyright 2024 The InfiniFlow Authors. All Rights Reserved.
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

"""
Unit tests for TaskHandler module.

Mock strategy: external boundaries (LLMBundle, model config services, settings)
are mocked so that ``handle()`` and ``_bind_embedding_model`` execute their
real logic.  Heavy orchestration methods (``_run_standard_chunking``,
``_run_raptor``, ``_run_graphrag``) are mocked since they are tested
exhaustively in the integration test suite.

Stable pure helpers (_build_toc) are tested directly.
"""

import pytest
from unittest.mock import MagicMock, AsyncMock, patch

from rag.svr.task_executor_refactor.task_handler import TaskHandler

# Reuse shared helpers from conftest
from test.unit_test.rag.svr.task_executor_refactor.conftest import (
    patch_embedding_binding,
    create_mock_settings,
    make_task_context,
)


async def _async_noop(*args, **kwargs):
    """Async no-op for mocking async functions without AsyncMock overhead."""
    return None


class TestTaskHandlerHandleTask:
    """Tests for the public handle_task() entry point."""

    @pytest.mark.asyncio
    async def test_handle_task_calls_handle(self):
        """Test handle_task delegates to handle()."""
        ctx = MagicMock()
        ctx.id = "task_1"
        ctx.tenant_id = "tenant_1"
        ctx.kb_id = "kb_1"
        ctx.doc_id = "doc_1"
        ctx.has_canceled_func = MagicMock(return_value=False)
        handler = TaskHandler(ctx=ctx)
        handler.handle = AsyncMock()
        await handler.handle_task()
        handler.handle.assert_called_once()

    @pytest.mark.asyncio
    async def test_handle_task_cleanup_on_cancel(self):
        """Test handle_task cleans up docStore when canceled."""
        from common import settings
        mock_doc_store = MagicMock()
        mock_doc_store.index_exist = MagicMock(return_value=True)
        mock_doc_store.delete = MagicMock(return_value=None)
        orig = settings.docStoreConn
        settings.docStoreConn = mock_doc_store
        try:
            ctx = MagicMock()
            ctx.id = "task_1"
            ctx.tenant_id = "tenant_1"
            ctx.kb_id = "kb_1"
            ctx.doc_id = "doc_1"
            ctx.has_canceled_func = MagicMock(return_value=True)
            ctx.recording_context = MagicMock()
            handler = TaskHandler(ctx=ctx)
            handler.handle = AsyncMock(side_effect=Exception("test error"))
            with pytest.raises(Exception, match="test error"):
                await handler.handle_task()
            mock_doc_store.delete.assert_called()
        finally:
            settings.docStoreConn = orig

    @pytest.mark.asyncio
    async def test_handle_task_cleanup_skips_when_index_missing(self):
        """Cancel cleanup should not call delete when the index doesn't exist."""
        from common import settings
        mock_doc_store = MagicMock()
        mock_doc_store.index_exist = MagicMock(return_value=False)
        mock_doc_store.delete = MagicMock()
        orig = settings.docStoreConn
        settings.docStoreConn = mock_doc_store
        try:
            ctx = MagicMock()
            ctx.id = "task_1"
            ctx.tenant_id = "tenant_1"
            ctx.kb_id = "kb_1"
            ctx.doc_id = "doc_1"
            ctx.has_canceled_func = MagicMock(return_value=True)
            ctx.recording_context = MagicMock()
            handler = TaskHandler(ctx=ctx)
            handler.handle = AsyncMock(side_effect=Exception("test error"))
            with pytest.raises(Exception, match="test error"):
                await handler.handle_task()
            mock_doc_store.delete.assert_not_called()
        finally:
            settings.docStoreConn = orig


class TestTaskHandlerHandle:
    """Tests for the public handle() method.

    External boundaries (LLMBundle, model config services, settings) are mocked
    so that ``_bind_embedding_model`` and ``_init_kb`` execute their real logic
    through ``handle()``.  Only the heavy orchestration methods
    (``_run_standard_chunking``, ``_run_raptor``, ``_run_graphrag``) are mocked.
    """

    # ── Context factory: make_task_context from conftest — see import above

    @pytest.mark.asyncio
    async def test_handle_memory_task(self):
        """Test handle returns after dispatching memory task — no further processing."""
        ctx = make_task_context(task_type="memory")
        ctx.raw_task = {"memory_id": "mem_1", "id": "task_1"}

        with patch("rag.svr.task_executor_refactor.task_handler.handle_save_to_memory_task",
                   new_callable=AsyncMock) as mock_handle:

            handler = TaskHandler(ctx=ctx)
            handler._run_standard_chunking = AsyncMock()
            handler._run_dataflow = AsyncMock()
            await handler.handle()

            mock_handle.assert_called_once_with(ctx.raw_task)
            # After memory task, should return immediately — no further routing
            handler._run_standard_chunking.assert_not_called()
            handler._run_dataflow.assert_not_called()

    @pytest.mark.asyncio
    async def test_handle_dataflow_task(self):
        """Test handle dispatches dataflow tasks (after embedding binding + init_kb)."""
        ctx = make_task_context(task_type="dataflow", doc_id="doc_1")

        with patch_embedding_binding(), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", create_mock_settings()), \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name", return_value="test_idx"):

            handler = TaskHandler(ctx=ctx)
            handler._run_dataflow = AsyncMock()
            await handler.handle()
            handler._run_dataflow.assert_called_once()

    @pytest.mark.asyncio
    async def test_handle_canceled_task(self):
        """Test handle returns early when task is canceled."""
        ctx = make_task_context(has_canceled_func=MagicMock(return_value=True))

        handler = TaskHandler(ctx=ctx)
        await handler.handle()
        ctx.progress_cb.assert_called_once_with(-1, msg="Task has been canceled.")

    @pytest.mark.asyncio
    async def test_handle_standard_chunking(self):
        """Test handle routes to standard chunking.

        ``_bind_embedding_model`` and ``_init_kb`` run their real code;
        only the external boundary (LLM API, settings) is mocked.
        """
        ctx = make_task_context()

        with patch_embedding_binding(), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", create_mock_settings()), \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name", return_value="test_idx"):

            handler = TaskHandler(ctx=ctx)
            handler._run_standard_chunking = AsyncMock()
            await handler.handle()
            handler._run_standard_chunking.assert_called_once()

    @pytest.mark.asyncio
    async def test_handle_raptor_task(self):
        """Test handle routes to RAPTOR with real embedding binding."""
        ctx = make_task_context(task_type="raptor")

        with patch_embedding_binding(), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", create_mock_settings()), \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name", return_value="test_idx"):

            handler = TaskHandler(ctx=ctx)
            handler._run_raptor = AsyncMock()
            await handler.handle()
            handler._run_raptor.assert_called_once()

    @pytest.mark.asyncio
    async def test_handle_graphrag_task(self):
        """Test handle routes to GraphRAG with real embedding binding."""
        ctx = make_task_context(task_type="graphrag")

        with patch_embedding_binding(), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", create_mock_settings()), \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name", return_value="test_idx"):

            handler = TaskHandler(ctx=ctx)
            handler._run_graphrag = AsyncMock()
            await handler.handle()
            handler._run_graphrag.assert_called_once()

    @pytest.mark.asyncio
    async def test_handle_embedding_model_failure(self):
        """Test handle returns early when embedding model binding fails.

        ``LLMBundle`` is patched to raise, so ``_bind_embedding_model``
        itself raises — no need to mock the private method.
        """
        ctx = make_task_context()

        with patch("rag.svr.task_executor_refactor.task_handler.get_model_config_from_provider_instance") as mock_cfg, \
             patch("rag.svr.task_executor_refactor.task_handler.get_tenant_default_model_by_type") as mock_default, \
             patch("rag.svr.task_executor_refactor.task_handler.LLMBundle") as mock_bundle:

            mock_cfg.return_value = MagicMock()
            mock_default.return_value = MagicMock()
            mock_bundle.side_effect = RuntimeError("embedding service unavailable")

            handler = TaskHandler(ctx=ctx)
            with pytest.raises(RuntimeError, match="embedding service unavailable"):
                await handler.handle()

    @pytest.mark.asyncio
    async def test_handle_storage_binary_none_raises_file_not_found(self):
        """Verify that None binary from storage raises FileNotFoundError."""
        ctx = make_task_context()

        with patch_embedding_binding(), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", create_mock_settings()), \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name", return_value="test_idx"), \
             patch("rag.svr.task_executor_refactor.task_handler.File2DocumentService.get_storage_address",
                   return_value=("bucket_test", "name_test")), \
             patch.object(TaskHandler, "_get_storage_binary", new_callable=AsyncMock, return_value=None):

            handler = TaskHandler(ctx=ctx)
            # Do NOT mock _run_standard_chunking — we want real code path for the check
            with pytest.raises(FileNotFoundError, match="Can not find file <test.pdf> from minio"):
                await handler.handle()


class TestTaskHandlerBuildToc:
    """Tests for _build_toc — stable pure helper (requires LLM mocking)."""

    def test_build_toc_with_empty_docs(self):
        """Test _build_toc returns None when run_toc_from_text returns empty."""
        ctx = MagicMock()
        ctx.tenant_id = "tenant_1"
        ctx.llm_id = "llm_1"
        ctx.language = "en"

        docs = [{"id": "chunk_1", "content_with_weight": "text", "page_num_int": [1], "top_int": [0]}]

        def mock_asyncio_run(coro):
            coro.close()
            return []

        with patch("rag.svr.task_executor_refactor.task_handler.get_model_config_from_provider_instance") as mock_cfg, \
             patch("rag.svr.task_executor_refactor.task_handler.LLMBundle") as mock_bundle, \
             patch("rag.svr.task_executor_refactor.task_handler.asyncio.run", side_effect=mock_asyncio_run):

            mock_cfg.return_value = MagicMock()
            mock_msg = MagicMock()
            mock_bundle.return_value.__enter__.return_value = mock_msg

            result = TaskHandler._build_toc(ctx, docs, MagicMock())
            assert result is None

    def test_build_toc_with_results(self):
        """Test _build_toc builds TOC chunk when results exist."""
        ctx = MagicMock()
        ctx.tenant_id = "tenant_1"
        ctx.llm_id = "llm_1"
        ctx.language = "en"

        docs = [{"id": "chunk_0", "content_with_weight": "text", "doc_id": "doc_1", "page_num_int": [1], "top_int": [0]}]
        toc_result = [{"chunk_id": "0", "title": "Section 1"}]

        def mock_asyncio_run(coro):
            coro.close()
            return toc_result

        with patch("rag.svr.task_executor_refactor.task_handler.get_model_config_from_provider_instance") as mock_cfg, \
             patch("rag.svr.task_executor_refactor.task_handler.LLMBundle") as mock_bundle, \
             patch("rag.svr.task_executor_refactor.task_handler.asyncio.run", side_effect=mock_asyncio_run):

            mock_cfg.return_value = MagicMock()
            mock_msg = MagicMock()
            mock_bundle.return_value.__enter__.return_value = mock_msg

            result = TaskHandler._build_toc(ctx, docs, MagicMock())
            assert result is not None
            assert "toc_kwd" in result
            assert result["toc_kwd"] == "toc"
            assert result["available_int"] == 0


@pytest.mark.filterwarnings("ignore::pytest.PytestUnraisableExceptionWarning")
class TestTaskHandlerEvaluation:
    """Tests for evaluation task routing and _run_evaluation implementation.

    Evaluation tasks must be dispatched BEFORE embedding model binding
    (matching the original do_handle_task behavior). They do not depend on
    embedding models at all.
    """

    # ── Routing tests ─────────────────────────────────────────────────────

    @pytest.mark.asyncio
    async def test_handle_routes_evaluation_before_embedding_bind(self):
        """Evaluation must be dispatched before _bind_embedding_model.

        If the embedding model is unavailable, evaluation should still succeed
        (it doesn't need an embedding model). The refactored handle() must
        check task_type == "evaluation" before calling _bind_embedding_model.
        """
        ctx = make_task_context(task_type="evaluation")

        handler = TaskHandler(ctx=ctx)
        handler._run_evaluation = AsyncMock()
        await handler.handle()
        handler._run_evaluation.assert_called_once()

    @pytest.mark.asyncio
    async def test_handle_evaluation_bypasses_embedding_model_failure(self):
        """Evaluation task should succeed even when embedding model binding fails."""
        ctx = make_task_context(task_type="evaluation")

        # Simulate embedding model failure — evaluation should not reach this code
        with patch("rag.svr.task_executor_refactor.task_handler.LLMBundle",
                   side_effect=RuntimeError("embedding unavailable")):
            handler = TaskHandler(ctx=ctx)
            handler._run_evaluation = AsyncMock()
            # Should NOT raise — evaluation runs before embed bind
            await handler.handle()
            handler._run_evaluation.assert_called_once()

    # ── Implementation tests ──────────────────────────────────────────────

    @pytest.mark.asyncio
    async def test_run_evaluation_calls_execute_run_all_cases(self):
        """_run_evaluation should call EvaluationService.execute_run_all_cases
        with the correct parameters from the task context.
        """
        ctx = make_task_context()
        ctx.get = MagicMock(side_effect=lambda k, d=None: {
            "eva_run_id": "run_001",
            "case_ids": ["case_1", "case_2"],
            "metrics_name": ["accuracy", "recall"],
        }.get(k, d))

        with patch(
            "rag.svr.task_executor_refactor.task_handler.EvaluationService"
        ) as mock_svc:
            mock_svc.get_test_cases_count.return_value = 10
            mock_svc.execute_run_all_cases = AsyncMock()

            handler = TaskHandler(ctx=ctx)
            await handler._run_evaluation()

            mock_svc.get_test_cases_count.assert_called_once_with("run_001")
            mock_svc.execute_run_all_cases.assert_called_once()
            call_kwargs = mock_svc.execute_run_all_cases.call_args
            assert call_kwargs[0][0] == "run_001"
            assert call_kwargs[1]["case_ids"] == ["case_1", "case_2"]
            assert call_kwargs[1]["metrics_name"] == ["accuracy", "recall"]

    @pytest.mark.asyncio
    async def test_run_evaluation_marks_failed_on_exception(self):
        """On exception, _run_evaluation should update EvaluationRun to FAILED
        and re-raise the exception.
        """
        ctx = make_task_context()
        ctx.get = MagicMock(side_effect=lambda k, d=None: {
            "eva_run_id": "run_001",
            "case_ids": [],
            "metrics_name": None,
        }.get(k, d))

        test_error = RuntimeError("evaluation timeout")

        with patch(
            "rag.svr.task_executor_refactor.task_handler.EvaluationService"
        ) as mock_svc:
            mock_svc.get_test_cases_count.return_value = 5
            mock_svc.execute_run_all_cases = AsyncMock(side_effect=test_error)

            with patch(
                "rag.svr.task_executor_refactor.task_handler.EvaluationRun"
            ) as mock_run:
                mock_update = MagicMock()
                mock_run.update.return_value = mock_update
                mock_update.where.return_value = mock_update

                handler = TaskHandler(ctx=ctx)
                with pytest.raises(RuntimeError, match="evaluation timeout"):
                    await handler._run_evaluation()

                # Verify EvaluationRun was updated to FAILED
                mock_run.update.assert_called_once()
                update_kwargs = mock_run.update.call_args[1]
                assert "status" in update_kwargs
                assert "complete_time" in update_kwargs
                mock_update.where.assert_called_once()
                mock_update.execute.assert_called_once()

    @pytest.mark.asyncio
    async def test_run_evaluation_timeout_formula_with_case_ids(self):
        """Timeout should be: (len(case_ids) + 1) * (len(metrics_name) + 1) * 60.

        When both case_ids and metrics_name are provided.
        """
        ctx = make_task_context()
        ctx.get = MagicMock(side_effect=lambda k, d=None: {
            "eva_run_id": "run_001",
            "case_ids": ["c1", "c2", "c3"],  # 3 cases
            "metrics_name": ["accuracy", "recall"],  # 2 metrics
        }.get(k, d))

        with patch(
            "rag.svr.task_executor_refactor.task_handler.EvaluationService"
        ) as mock_svc:
            mock_svc.get_test_cases_count.return_value = 100
            mock_svc.execute_run_all_cases = AsyncMock()

            with patch("rag.svr.task_executor_refactor.task_handler.asyncio.wait_for", side_effect=_async_noop) as mock_wait:

                handler = TaskHandler(ctx=ctx)
                await handler._run_evaluation()

                # Expected: (3 + 1) * 2 * 60 = 480
                assert mock_wait.call_args[1]["timeout"] == 480

    @pytest.mark.asyncio
    async def test_run_evaluation_timeout_formula_without_case_ids(self):
        """Timeout should be: (cases_total + 1) * (3) * 60 when case_ids is empty.

        When case_ids is empty and metrics_name is None.
        """
        ctx = make_task_context()
        ctx.get = MagicMock(side_effect=lambda k, d=None: {
            "eva_run_id": "run_001",
            "case_ids": [],
            "metrics_name": None,
        }.get(k, d))

        with patch(
            "rag.svr.task_executor_refactor.task_handler.EvaluationService"
        ) as mock_svc:
            mock_svc.get_test_cases_count.return_value = 50  # cases_total
            mock_svc.execute_run_all_cases = AsyncMock()

            with patch("rag.svr.task_executor_refactor.task_handler.asyncio.wait_for", side_effect=_async_noop) as mock_wait:

                handler = TaskHandler(ctx=ctx)
                await handler._run_evaluation()

                # Expected: (50 + 1) * 3 * 60 = 9180
                assert mock_wait.call_args[1]["timeout"] == 9180


@pytest.mark.filterwarnings("ignore::pytest.PytestUnraisableExceptionWarning")
class TestTaskHandlerReembedding:
    """Tests for reembedding task routing and _run_reembedding implementation.

    Reembedding scrolls through all existing chunks in the knowledge base and
    re-encodes them with a new (target) embedding model, then updates the
    document store with the new vectors.
    """

    # ── Implementation tests ──────────────────────────────────────────────

    @pytest.mark.asyncio
    async def test_run_reembedding_no_chunks_returns_early(self):
        """When the KB has zero chunks, reembedding should report done and return."""
        ctx = make_task_context()
        ctx.get = MagicMock(side_effect=lambda k, d=None: {
            "target_embed_id": "embd_target",
        }.get(k, d))

        mock_settings = create_mock_settings()
        mock_settings.docStoreConn.search.return_value = "fake_es_result"
        mock_settings.docStoreConn.get_total.return_value = 0

        with patch("rag.svr.task_executor_refactor.task_handler.get_model_config_from_provider_instance",
                   return_value=MagicMock()), \
             patch("rag.svr.task_executor_refactor.task_handler.LLMBundle",
                   return_value=MagicMock()), \
             patch("rag.svr.task_executor_refactor.task_handler.is_strong_enough",
                   new=_async_noop), \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name",
                   return_value="test_idx"), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", mock_settings):

            handler = TaskHandler(ctx=ctx)
            await handler._run_reembedding(original_vector_size=128)

            # Progress should report done
            ctx.progress_cb.assert_called()
            call_args = ctx.progress_cb.call_args_list[0]
            assert "Embedding switching done" in call_args[1]["msg"]

    @pytest.mark.asyncio
    async def test_run_reembedding_canceled_task_returns_early(self):
        """When the task is canceled mid-processing, reembedding should stop."""
        ctx = make_task_context()
        ctx.get = MagicMock(side_effect=lambda k, d=None: {
            "target_embed_id": "embd_target",
        }.get(k, d))
        ctx.has_canceled_func = MagicMock(return_value=True)  # canceled

        mock_settings = create_mock_settings()
        mock_settings.docStoreConn.search.return_value = "fake_es_result"
        mock_settings.docStoreConn.get_total.return_value = 10
        mock_settings.docStoreConn.scroll.return_value = [{"hits": ["dummy"]}]

        with patch("rag.svr.task_executor_refactor.task_handler.get_model_config_from_provider_instance",
                   return_value=MagicMock()), \
             patch("rag.svr.task_executor_refactor.task_handler.LLMBundle",
                   return_value=MagicMock()), \
             patch("rag.svr.task_executor_refactor.task_handler.is_strong_enough",
                   new=_async_noop), \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name",
                   return_value="test_idx"), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", mock_settings):

            handler = TaskHandler(ctx=ctx)
            await handler._run_reembedding(original_vector_size=128)

            # Should report canceled
            cancel_calls = [
                c for c in ctx.progress_cb.call_args_list
                if c[1].get("msg") == "Task has been canceled."
            ]
            assert len(cancel_calls) == 1

    @pytest.mark.asyncio
    async def test_run_reembedding_updates_es_with_new_vectors(self):
        """After re-embedding, each chunk should be updated in ES with the new
        vector fields, and old vector fields removed when dimensions differ.
        """
        ctx = make_task_context()
        ctx.get = MagicMock(side_effect=lambda k, d=None: {
            "target_embed_id": "embd_target",
        }.get(k, d))
        ctx.has_canceled_func = MagicMock(return_value=False)

        # Create mock chunks returned by get_fields
        chunk_1 = {"id": "chunk_1", "question_kwd": ["q1"],
                    "content_with_weight": "content1", "docnm_kwd": "doc1",
                    "q_128_vec": [0.1] * 128}
        chunk_2 = {"id": "chunk_2", "question_kwd": ["q2"],
                    "content_with_weight": "content2", "docnm_kwd": "doc2",
                    "q_128_vec": [0.2] * 128}

        mock_settings = create_mock_settings()
        mock_settings.docStoreConn.search.return_value = "fake_es_result"
        mock_settings.docStoreConn.get_total.return_value = 2
        mock_settings.docStoreConn.scroll.return_value = [
            {"hits": {"chunk_1": chunk_1.copy(), "chunk_2": chunk_2.copy()}}
        ]
        mock_settings.docStoreConn.get_fields.return_value = {
            "chunk_1": chunk_1.copy(),
            "chunk_2": chunk_2.copy(),
        }
        mock_settings.docStoreConn.update.return_value = True

        # Mock EmbeddingService to return a different vector_size (256 vs 128)
        with patch("rag.svr.task_executor_refactor.task_handler.get_model_config_from_provider_instance",
                   return_value=MagicMock()), \
             patch("rag.svr.task_executor_refactor.task_handler.LLMBundle",
                   return_value=MagicMock()), \
             patch("rag.svr.task_executor_refactor.task_handler.is_strong_enough",
                   new=_async_noop), \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name",
                   return_value="test_idx"), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", mock_settings), \
             patch("rag.svr.task_executor_refactor.task_handler.EmbeddingService") as mock_es:

            mock_es_instance = MagicMock()
            mock_es_instance.embed_chunks = AsyncMock(return_value=(500, 256))
            mock_es.return_value = mock_es_instance

            handler = TaskHandler(ctx=ctx)
            await handler._run_reembedding(original_vector_size=128)

            # Verify ES update was called for both chunks
            assert mock_settings.docStoreConn.update.call_count == 2
            # First update call should have "remove": "q_128_vec" (old size)
            first_call = mock_settings.docStoreConn.update.call_args_list[0]
            assert "remove" in first_call[0][1]

    @pytest.mark.asyncio
    async def test_run_reembedding_retry_on_embedding_failure(self):
        """When embedding fails transiently, it should retry up to 3 times."""
        ctx = make_task_context()
        ctx.get = MagicMock(side_effect=lambda k, d=None: {
            "target_embed_id": "embd_target",
        }.get(k, d))
        ctx.has_canceled_func = MagicMock(return_value=False)

        chunk_1 = {"id": "chunk_1", "question_kwd": ["q1"],
                    "content_with_weight": "content1", "docnm_kwd": "doc1",
                    "q_128_vec": [0.1] * 128}

        mock_settings = create_mock_settings()
        mock_settings.docStoreConn.search.return_value = "fake_es_result"
        mock_settings.docStoreConn.get_total.return_value = 1
        mock_settings.docStoreConn.scroll.return_value = [
            {"hits": {"chunk_1": chunk_1.copy()}}
        ]
        mock_settings.docStoreConn.get_fields.return_value = {
            "chunk_1": chunk_1.copy(),
        }
        mock_settings.docStoreConn.update.return_value = True

        with patch("rag.svr.task_executor_refactor.task_handler.get_model_config_from_provider_instance",
                   return_value=MagicMock()), \
             patch("rag.svr.task_executor_refactor.task_handler.LLMBundle",
                   return_value=MagicMock()), \
             patch("rag.svr.task_executor_refactor.task_handler.is_strong_enough",
                   new=_async_noop), \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name",
                   return_value="test_idx"), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", mock_settings), \
             patch("rag.svr.task_executor_refactor.task_handler.EmbeddingService") as mock_es, \
             patch("rag.svr.task_executor_refactor.task_handler.asyncio.sleep", new=_async_noop):

            # First 2 calls fail, 3rd succeeds
            mock_es_instance = MagicMock()
            mock_es_instance.embed_chunks = AsyncMock(side_effect=[
                RuntimeError("transient failure"),
                RuntimeError("transient failure"),
                (500, 256),
            ])
            mock_es.return_value = mock_es_instance

            handler = TaskHandler(ctx=ctx)
            await handler._run_reembedding(original_vector_size=128)

            # Embedding should have been attempted 3 times
            assert mock_es_instance.embed_chunks.call_count == 3
            # Should eventually succeed and update ES
            assert mock_settings.docStoreConn.update.call_count == 1

    @pytest.mark.asyncio
    async def test_run_reembedding_raises_after_all_retries_exhausted(self):
        """When all 3 retry attempts fail, it should raise the exception."""
        ctx = make_task_context()
        ctx.get = MagicMock(side_effect=lambda k, d=None: {
            "target_embed_id": "embd_target",
        }.get(k, d))
        ctx.has_canceled_func = MagicMock(return_value=False)

        chunk_1 = {"id": "chunk_1", "question_kwd": ["q1"],
                    "content_with_weight": "content1", "docnm_kwd": "doc1"}

        mock_settings = create_mock_settings()
        mock_settings.docStoreConn.search.return_value = "fake_es_result"
        mock_settings.docStoreConn.get_total.return_value = 1
        mock_settings.docStoreConn.scroll.return_value = [
            {"hits": {"chunk_1": chunk_1.copy()}}
        ]
        mock_settings.docStoreConn.get_fields.return_value = {"chunk_1": chunk_1.copy()}

        fatal_error = RuntimeError("embedding service down")

        with patch("rag.svr.task_executor_refactor.task_handler.get_model_config_from_provider_instance",
                   return_value=MagicMock()), \
             patch("rag.svr.task_executor_refactor.task_handler.LLMBundle",
                   return_value=MagicMock()), \
             patch("rag.svr.task_executor_refactor.task_handler.is_strong_enough",
                   new=_async_noop), \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name",
                   return_value="test_idx"), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", mock_settings), \
             patch("rag.svr.task_executor_refactor.task_handler.EmbeddingService") as mock_es, \
             patch("rag.svr.task_executor_refactor.task_handler.asyncio.sleep", new=_async_noop):

            mock_es_instance = MagicMock()
            mock_es_instance.embed_chunks = AsyncMock(side_effect=fatal_error)
            mock_es.return_value = mock_es_instance

            handler = TaskHandler(ctx=ctx)
            with pytest.raises(RuntimeError, match="embedding service down"):
                await handler._run_reembedding(original_vector_size=128)


@pytest.mark.filterwarnings("ignore::pytest.PytestUnraisableExceptionWarning")
class TestTaskHandlerClone:
    """Tests for clone task routing and _run_clone implementation.

    Cloning copies documents and their chunks from a source KB to a target KB.
    It first clones document records and metadata at the DB layer, then clones
    individual chunks in the document store.
    """

    # ── Routing tests ─────────────────────────────────────────────────────

    @pytest.mark.asyncio
    async def test_handle_routes_clone(self):
        """task_type='clone' should route to _run_clone."""
        ctx = make_task_context(task_type="clone")

        with patch_embedding_binding(), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", create_mock_settings()), \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name", return_value="test_idx"):

            handler = TaskHandler(ctx=ctx)
            handler._run_clone = AsyncMock()
            await handler.handle()
            handler._run_clone.assert_called_once()

    # ── Implementation tests ──────────────────────────────────────────────

    @pytest.mark.asyncio
    async def test_run_clone_calls_document_services(self):
        """_run_clone should call DocumentService.clone_kb and
        DocMetadataService.clone_document_metadata with correct parameters.
        """
        ctx = make_task_context()
        ctx.get = MagicMock(side_effect=lambda k, d=None: {
            "target_kb_id": "target_kb_1",
        }.get(k, d))

        mock_settings = create_mock_settings()
        mock_settings.docStoreConn.search.return_value = "fake_es_result"
        mock_settings.docStoreConn.get_total.return_value = 0  # no chunks

        with patch(
            "rag.svr.task_executor_refactor.task_handler.DocumentService"
        ) as mock_doc_svc, \
             patch(
            "rag.svr.task_executor_refactor.task_handler.DocMetadataService"
        ) as mock_meta_svc, \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name",
                   return_value="test_idx"), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", mock_settings):

            mock_doc_svc.clone_kb.return_value = {"old_doc_1": "new_doc_1"}

            handler = TaskHandler(ctx=ctx)
            await handler._run_clone()

            mock_doc_svc.clone_kb.assert_called_once_with("kb_1", "target_kb_1", "tenant_1")
            mock_meta_svc.clone_document_metadata.assert_called_once_with(
                "kb_1", "target_kb_1", {"old_doc_1": "new_doc_1"}, "tenant_1"
            )

    @pytest.mark.asyncio
    async def test_run_clone_no_chunks_returns_early(self):
        """When the source KB has zero chunks, clone should report done and return."""
        ctx = make_task_context()
        ctx.get = MagicMock(side_effect=lambda k, d=None: {
            "target_kb_id": "target_kb_1",
        }.get(k, d))

        mock_settings = create_mock_settings()
        mock_settings.docStoreConn.search.return_value = "fake_es_result"
        mock_settings.docStoreConn.get_total.return_value = 0

        with patch(
            "rag.svr.task_executor_refactor.task_handler.DocumentService"
        ) as mock_doc_svc, \
             patch(
            "rag.svr.task_executor_refactor.task_handler.DocMetadataService"), \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name",
                   return_value="test_idx"), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", mock_settings):

            mock_doc_svc.clone_kb.return_value = {}

            handler = TaskHandler(ctx=ctx)
            await handler._run_clone()

            # Should report done early
            done_calls = [
                c for c in ctx.progress_cb.call_args_list
                if "Chunks cloning done" in str(c)
            ]
            assert len(done_calls) == 1

    @pytest.mark.asyncio
    async def test_run_clone_canceled_task_returns_early(self):
        """When the task is canceled during cloning, it should stop processing."""
        ctx = make_task_context()
        ctx.get = MagicMock(side_effect=lambda k, d=None: {
            "target_kb_id": "target_kb_1",
        }.get(k, d))
        ctx.has_canceled_func = MagicMock(return_value=True)  # canceled

        mock_settings = create_mock_settings()
        mock_settings.docStoreConn.search.return_value = "fake_es_result"
        mock_settings.docStoreConn.get_total.return_value = 10
        mock_settings.docStoreConn.scroll.return_value = [{"hits": {"chunk_1": {}}}]
        mock_settings.docStoreConn.get_fields.return_value = {"chunk_1": {"docnm_kwd": "doc1"}}

        with patch(
            "rag.svr.task_executor_refactor.task_handler.DocumentService"
        ) as mock_doc_svc, \
             patch(
            "rag.svr.task_executor_refactor.task_handler.DocMetadataService"), \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name",
                   return_value="test_idx"), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", mock_settings):

            mock_doc_svc.clone_kb.return_value = {}

            handler = TaskHandler(ctx=ctx)
            await handler._run_clone()

            # Should report canceled
            cancel_calls = [
                c for c in ctx.progress_cb.call_args_list
                if c[1].get("msg") == "Task has been canceled."
            ]
            assert len(cancel_calls) == 1

    @pytest.mark.asyncio
    async def test_run_clone_calls_clone_doc_for_each_chunk(self):
        """Each chunk in the source KB should be cloned via docStoreConn.clone_doc."""
        ctx = make_task_context()
        ctx.get = MagicMock(side_effect=lambda k, d=None: {
            "target_kb_id": "target_kb_1",
        }.get(k, d))
        ctx.has_canceled_func = MagicMock(return_value=False)

        mock_settings = create_mock_settings()
        mock_settings.docStoreConn.search.return_value = "fake_es_result"
        mock_settings.docStoreConn.get_total.return_value = 3
        mock_settings.docStoreConn.scroll.return_value = [
            {"hits": {"chunk_1": {}, "chunk_2": {}, "chunk_3": {}}}
        ]
        mock_settings.docStoreConn.get_fields.return_value = {
            "chunk_1": {"docnm_kwd": "doc1"},
            "chunk_2": {"docnm_kwd": "doc2"},
            "chunk_3": {"docnm_kwd": "doc3"},
        }
        mock_settings.docStoreConn.clone_doc.return_value = True

        with patch(
            "rag.svr.task_executor_refactor.task_handler.DocumentService"
        ) as mock_doc_svc, \
             patch(
            "rag.svr.task_executor_refactor.task_handler.DocMetadataService"), \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name",
                   return_value="test_idx"), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", mock_settings):

            mock_doc_svc.clone_kb.return_value = {"old_doc_1": "new_doc_1"}

            handler = TaskHandler(ctx=ctx)
            await handler._run_clone()

            # clone_doc should be called once per chunk
            assert mock_settings.docStoreConn.clone_doc.call_count == 3

    @pytest.mark.asyncio
    async def test_run_clone_retry_on_clone_doc_failure(self):
        """When clone_doc fails transiently, it should retry up to 3 times."""
        ctx = make_task_context()
        ctx.get = MagicMock(side_effect=lambda k, d=None: {
            "target_kb_id": "target_kb_1",
        }.get(k, d))
        ctx.has_canceled_func = MagicMock(return_value=False)

        mock_settings = create_mock_settings()
        mock_settings.docStoreConn.search.return_value = "fake_es_result"
        mock_settings.docStoreConn.get_total.return_value = 1
        mock_settings.docStoreConn.scroll.return_value = [
            {"hits": {"chunk_1": {}}}
        ]
        mock_settings.docStoreConn.get_fields.return_value = {
            "chunk_1": {"docnm_kwd": "doc1"},
        }
        # First 2 calls fail, 3rd succeeds
        mock_settings.docStoreConn.clone_doc.side_effect = [
            RuntimeError("transient ES error"),
            RuntimeError("transient ES error"),
            True,
        ]

        with patch(
            "rag.svr.task_executor_refactor.task_handler.DocumentService"
        ) as mock_doc_svc, \
             patch(
            "rag.svr.task_executor_refactor.task_handler.DocMetadataService"), \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name",
                   return_value="test_idx"), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", mock_settings), \
             patch("rag.svr.task_executor_refactor.task_handler.asyncio.sleep", new=_async_noop):

            mock_doc_svc.clone_kb.return_value = {}

            handler = TaskHandler(ctx=ctx)
            await handler._run_clone()

            # clone_doc should have been called 3 times (2 failures + 1 success)
            assert mock_settings.docStoreConn.clone_doc.call_count == 3

    @pytest.mark.asyncio
    async def test_run_clone_raises_after_all_retries_exhausted(self):
        """When all 3 clone_doc retries fail, it should raise the exception."""
        ctx = make_task_context()
        ctx.get = MagicMock(side_effect=lambda k, d=None: {
            "target_kb_id": "target_kb_1",
        }.get(k, d))
        ctx.has_canceled_func = MagicMock(return_value=False)

        fatal_error = RuntimeError("ES cluster unreachable")

        mock_settings = create_mock_settings()
        mock_settings.docStoreConn.search.return_value = "fake_es_result"
        mock_settings.docStoreConn.get_total.return_value = 1
        mock_settings.docStoreConn.scroll.return_value = [
            {"hits": {"chunk_1": {}}}
        ]
        mock_settings.docStoreConn.get_fields.return_value = {
            "chunk_1": {"docnm_kwd": "doc1"},
        }
        mock_settings.docStoreConn.clone_doc.side_effect = fatal_error

        with patch(
            "rag.svr.task_executor_refactor.task_handler.DocumentService"
        ) as mock_doc_svc, \
             patch(
            "rag.svr.task_executor_refactor.task_handler.DocMetadataService"), \
             patch("rag.svr.task_executor_refactor.task_handler.search.index_name",
                   return_value="test_idx"), \
             patch("rag.svr.task_executor_refactor.task_handler.settings", mock_settings), \
             patch("rag.svr.task_executor_refactor.task_handler.asyncio.sleep", new=_async_noop):

            mock_doc_svc.clone_kb.return_value = {}

            handler = TaskHandler(ctx=ctx)
            with pytest.raises(RuntimeError, match="ES cluster unreachable"):
                await handler._run_clone()


class TestTaskHandlerInit:
    """Tests for TaskHandler initialization."""

    def test_init_stores_context_and_hook(self):
        ctx = MagicMock()
        hook = MagicMock()
        handler = TaskHandler(ctx=ctx, billing_hook=hook)
        assert handler._task_context is ctx
        assert handler._billing_hook is hook

    def test_init_default_hook_none(self):
        ctx = MagicMock()
        handler = TaskHandler(ctx=ctx)
        assert handler._billing_hook is None
