import asyncio
import logging
from dataclasses import dataclass
from typing import Any, AsyncGenerator, Callable, Iterable
import io
from pathlib import Path
import time
import lark_oapi as lark
from lark_oapi.api.drive.v1 import (
    CreateExportTaskRequest,
    CreateExportTaskResponse,
    DownloadExportTaskRequest,
    DownloadExportTaskResponse,
    DownloadFileRequest,
    DownloadFileResponse,
    ExportTask,
    GetExportTaskRequest,
    GetExportTaskResponse,
    ListFileRequest,
    ListFileResponse,
)
from lark_oapi.api.bitable.v1 import ListAppTableRequest, ListAppTableResponse
from lark_oapi.api.sheets.v3 import QuerySpreadsheetSheetRequest
from lark_oapi.api.wiki.v2 import (
    GetNodeSpaceRequest,
    GetNodeSpaceResponse,
    ListSpaceNodeRequest,
)
from common.data_source.exceptions import ConnectorMissingCredentialError, ConnectorValidationError
from common.data_source.interfaces import LoadConnector, PollConnector
from common.data_source.models import Document, SecondsSinceUnixEpoch
from common.data_source.config import DocumentSource
from common.data_source.utils import get_file_ext, sanitize_filename

EXPORT_EXTENSIONS = {
    "doc": "docx",
    "docx": "docx",
    "sheet": "csv",
    "bitable": "csv",
    "file": "",
    "folder": ""
}


class LarkConnector(LoadConnector, PollConnector):
    """
    Minimal Lark connector scaffold.

    This only wires credential loading, basic validation, and empty
    load/poll hooks for future implementation.
    """

    @dataclass(frozen=True)
    class _NormalizedNode:
        edit_time: Any
        ftype: str
        ftoken: str
        fname: str | None
        created_time: Any
        children: list

    _ASYNC_NODE_CONCURRENCY = 5
    _DOWNLOAD_TIMEOUT_SECS = 10 * 60

    def __init__(self, token_type, folder_token, log_level = lark.LogLevel.ERROR) -> None:
        self.client: lark.Client | None = None
        self.app_id: str | None = None
        self.app_secret: str | None = None
        self.token_type = token_type
        self.folder_token = folder_token
        self.log_level = log_level

    # -------------------------
    # Credentials
    # -------------------------


    def load_credentials(self, credentials: dict[str, Any]) -> dict[str, Any] | None:
        self.app_id = credentials.get("app_id")
        self.app_secret = credentials.get("app_secret")
        log_level = self.log_level

        if not self.app_id or not self.app_secret:
            raise ConnectorValidationError("Missing Lark app_id or app_secret")

        self.client = (
            lark.Client.builder()
            .app_id(self.app_id)
            .app_secret(self.app_secret)
            .log_level(log_level)
            .build()
        )
        return None


    def validate_connector_settings(self) -> None:
        if not self.app_id or not self.app_secret:
            raise ConnectorValidationError("Lark credentials not loaded")

    # ---------------------------------
    # Load Files in sync_data_source
    # ---------------------------------

    def _debug_write_file(self, file_name: str, response) -> None:
        if self.log_level != lark.LogLevel.DEBUG:
            return
        desktop_dir = Path.home() / "Desktop"
        if not desktop_dir.exists():
            desktop_dir = Path.home() / "桌面"
        if not desktop_dir.exists():
            desktop_dir = Path.home()
        base_dir = desktop_dir / "debug_downloads"
        folder_name = f"{self.token_type}{self.folder_token[:8]}"
        file_path = base_dir / folder_name / file_name

        file_path.parent.mkdir(parents=True, exist_ok=True)
        content = response.file.read()
        with open(file_path, "wb") as file_handle:
            file_handle.write(content)
        response.file = io.BytesIO(content)


    async def _list_files_async(self, folder_token, page_size: int | None = None) -> list[Any]:
        if not folder_token:
            logging.info("No folder_token provided")
            return []

        files: list[Any] = []
        page_token: str | None = None

        while True:
            b = (
                ListFileRequest.builder()
                .folder_token(folder_token)
                .order_by("EditedTime")
                .direction("DESC")
            )
            if page_size is not None:
                b = b.page_size(page_size)
            if page_token:
                b = b.page_token(page_token)

            request: ListFileRequest = b.build()

            response: ListFileResponse = await asyncio.to_thread(
                self.client.drive.v1.file.list,
                request,
            )

            if not response.success():
                lark.logger.error(
                    f"client.drive.v1.file.list failed, "
                    f"code: {response.code}, "
                    f"msg: {response.msg}, "
                    f"log_id: {response.get_log_id()}, "
                )
                return []

            data = response.data
            items = getattr(data, "files", None) if data else None
            if items:
                files.extend(items)

            if not getattr(data, "has_more", False):
                break
            page_token = getattr(data, "next_page_token", None)

        return files


    def _build_document(
        self,
        fname,
        ftype,
        fsize,
        fmodified_time,
        fcreated_time,
        folder_path,
        response,
    ) -> Document:
        
        file_name = sanitize_filename(fname)

        title = f"{folder_path} / {file_name}" if folder_path else file_name

        # Only if debug mode
        self._debug_write_file(file_name, response)

        return Document(
            id=f"lark:{file_name}/{ftype}",
            blob=response.file.read(),
            source=DocumentSource.LARK,
            semantic_identifier=title,
            extension=get_file_ext(file_name),
            doc_updated_at=fmodified_time,
            size_bytes=fsize,
            metadata={"created_at": fcreated_time},
        )

    def _adapt_drive_node(self, file, children=[]) -> _NormalizedNode:
        return self._NormalizedNode(
            edit_time=file.modified_time,
            ftype=file.type,
            ftoken=file.token,
            fname=file.name,
            created_time=file.created_time,
            children=children
        )

    def _adapt_wiki_node(self, file, children=[]) -> _NormalizedNode:
        return self._NormalizedNode(
            edit_time=file.obj_edit_time,
            ftype=file.obj_type,
            ftoken=file.obj_token,
            fname=file.title,
            created_time=file.obj_create_time,
            children=children
        )


    async def _download_file_async(
        self,
        fname,
        ftype,
        ftoken,
        fmodified_time,
        fcreated_time,
        folder_path,
    ):
        request: DownloadFileRequest = (
            DownloadFileRequest.builder()
            .file_token(ftoken)
            .build()
        )
        try:
            response: DownloadFileResponse = await asyncio.wait_for(
                asyncio.to_thread(
                    self.client.drive.v1.file.download,
                    request,
                ),
                timeout=self._DOWNLOAD_TIMEOUT_SECS,
            )
        except asyncio.TimeoutError:
            lark.logger.error(
                "client.drive.v1.file.download timed out after %s seconds",
                self._DOWNLOAD_TIMEOUT_SECS,
            )
            return None
        if not response.success():
            lark.logger.error(
                f"client.drive.v1.file.download failed, code: {response.code}")
            return None

        return self._build_document(
            fname,
            ftype,
            response.raw.headers.get("Content-Length"),
            fmodified_time,
            fcreated_time,
            folder_path,
            response,
        )

    def _create_export_task(
        self,
        file_token: str,
        file_type: str,
        sub_id: str = None,
        file_extension: str = "csv",
    ):
        body = (
            ExportTask.builder()
            .file_extension(file_extension)
            .token(file_token)
            .type(file_type)
        )
        if sub_id:
            body = body.sub_id(sub_id)

        request: CreateExportTaskRequest = (
            CreateExportTaskRequest.builder()
            .request_body(body.build())
            .build()
        )

        response: CreateExportTaskResponse = self.client.drive.v1.export_task.create(
            request
        )

        if not response.success():
            lark.logger.error(
                f"client.drive.v1.export_task.create failed, "
                f"code: {response.code}, "
                f"msg: {response.msg}, "
                f"log_id: {response.get_log_id()}, "
            )
            return None

        return response.data.ticket


    async def _download_ticket_async(self, new_file):
        request: DownloadExportTaskRequest = (
            DownloadExportTaskRequest.builder()
            .file_token(new_file.result.file_token)
            .build()
        )

        try:
            response: DownloadExportTaskResponse = await asyncio.wait_for(
                asyncio.to_thread(
                    self.client.drive.v1.export_task.download,
                    request,
                ),
                timeout=self._DOWNLOAD_TIMEOUT_SECS,
            )
        except asyncio.TimeoutError:
            lark.logger.error(
                "client.drive.v1.export_task.download timed out after %s seconds",
                self._DOWNLOAD_TIMEOUT_SECS,
            )
            return None

        if not response.success():
            lark.logger.error(
                f"client.drive.v1.export_task.download failed, "
                f"code: {response.code}, "
                f"msg: {response.msg}, "
                f"log_id: {response.get_log_id()}, "
            )
            return None

        return response


    def _get_export_task_ticket(
        self,
        ticket: str,
        file_token: str,
        poll_interval_seconds: float = 1.0,
        timeout_seconds: float = 60.0,
    ):
        start_time = time.monotonic()
        while True:
            request: GetExportTaskRequest = (
                GetExportTaskRequest.builder()
                .ticket(ticket)
                .token(file_token)
                .build()
            )
            response: GetExportTaskResponse = self.client.drive.v1.export_task.get(
                request
            )

            if not response.success():
                lark.logger.error(
                    f"client.drive.v1.export_task.get failed, "
                    f"code: {response.code}, "
                    f"msg: {response.msg}, "
                    f"log_id: {response.get_log_id()}, "
                )
                return None

            data = response.data
            result = getattr(data, "result", None)
            job_status = getattr(result, "job_status", None) if result else None

            if job_status == 0:
                lark.logger.info(lark.JSON.marshal(data, indent=4))
                return data
            if job_status == 2:
                if time.monotonic() - start_time >= timeout_seconds:
                    lark.logger.error(
                        "export_task.get timeout for ticket=%s after %.2fs",
                        ticket,
                        time.monotonic() - start_time,
                    )
                    return None
                time.sleep(poll_interval_seconds)
                continue

            lark.logger.error(
                "export_task.get failed for ticket=%s job_status=%s",
                ticket,
                job_status,
            )
            return None

    
    async def _get_sheet_sub_id_async(self, spreadsheet_token: str) -> str | None:
        request: QuerySpreadsheetSheetRequest = (
            QuerySpreadsheetSheetRequest.builder()
            .spreadsheet_token(spreadsheet_token)
            .build()
        )

        response = await asyncio.to_thread(
            self.client.sheets.v3.spreadsheet_sheet.query,
            request,
        )

        if not response.success():
            lark.logger.error(
                "client.sheets.v3.spreadsheet_sheet.query failed, "
                f"code: {response.code}, "
                f"msg: {response.msg}, "
                f"log_id: {response.get_log_id()}, "
            )
            return None

        return response.data


    async def _get_bitable_sub_id_async(
        self, app_token: str, page_size: int | None = None, page_token: str | None = None
    ) -> str | None:
        body = ListAppTableRequest.builder().app_token(app_token)

        if page_size is not None:
            body = body.page_size(page_size)
        if page_token:
            body = body.page_token(page_token)

        request: ListAppTableRequest = body.build()

        response: ListAppTableResponse = await asyncio.to_thread(
            self.client.bitable.v1.app_table.list,
            request,
        )

        if not response.success():
            lark.logger.error(
                "client.bitable.v1.app_table.list failed, "
                f"code: {response.code}, "
                f"msg: {response.msg}, "
                f"log_id: {response.get_log_id()}, "
            )
            return None

        return response.data


    def _get_wiki_space_id(self, token: str) -> str | None:
        request: GetNodeSpaceRequest = (
            GetNodeSpaceRequest.builder()
            .token(token)
            .obj_type("wiki")
            .build()
        )

        response: GetNodeSpaceResponse = self.client.wiki.v2.space.get_node(request)

        if not response.success():
            lark.logger.error(
                "client.wiki.v2.space.get_node failed, "
                f"code: {response.code}, "
                f"msg: {response.msg}, "
                f"log_id: {response.get_log_id()}, "
            )
            return None

        return response.data


    async def _list_wiki_space_nodes_async(
        self,
        space_token: str,
        page_size: int | None = None,
    ) -> list[Any]:
        if not space_token:
            logging.info("No wiki space_token provided")
            return []

        space_info = await asyncio.to_thread(self._get_wiki_space_id, space_token)
        space_id = space_info.node.space_id

        nodes: list[Any] = []
        page_token: str | None = None

        while True:
            body = ListSpaceNodeRequest.builder().space_id(space_id)
            if page_size is not None:
                body = body.page_size(page_size)
            if page_token:
                body = body.page_token(page_token)
            request: ListSpaceNodeRequest = body.build()

            response = await asyncio.to_thread(
                self.client.wiki.v2.space_node.list,
                request,
            )

            if not response.success():
                lark.logger.error(
                    "client.wiki.v2.space_node.list failed, "
                    f"code: {response.code}, "
                    f"msg: {response.msg}, "
                    f"log_id: {response.get_log_id()}, "
                )
                return []

            data = response.data
            items = getattr(data, "items", None) if data else None
            if items:
                nodes.extend(items)

            if not getattr(data, "has_more", False):
                break
            page_token = getattr(data, "page_token", None)

        return nodes


    async def _export_and_download_async(
        self,
        token,
        *,
        src_type: str,
        file_extension: str,
        sub_id: str | None = None
    ) -> tuple[DownloadExportTaskResponse | None, Any | None]:
        ticket = self._create_export_task(
            token,
            src_type,
            sub_id=sub_id,
            file_extension=file_extension,
        )

        if not ticket:
            return None, None

        new_file = self._get_export_task_ticket(
            ticket,
            token,
        )

        if not new_file:
            return None, None
        response = await self._download_ticket_async(new_file)
        return response, new_file

    @staticmethod
    def _in_time_range(
        edit_time,
        time_range_start: SecondsSinceUnixEpoch | None,
        time_range_end: SecondsSinceUnixEpoch | None,
    ) -> bool:
        if not edit_time:
            return True
        ts = float(edit_time)
        if time_range_start is not None and ts <= time_range_start:
            return False
        if time_range_end is not None and ts > time_range_end:
            return False
        return True


    async def _walk_nodes_async(
        self,
        nodes: Iterable[_NormalizedNode],
        *,
        path: str | None = "",
        time_range_start: SecondsSinceUnixEpoch | None = None,
        time_range_end: SecondsSinceUnixEpoch | None = None,
        recurse_hook: Callable[[_NormalizedNode, str | None], Any] | None = None,
    ):
        batch: list[Document | None] = []
        file_jobs: list[tuple[int, tuple]] = []

        sem = asyncio.Semaphore(self._ASYNC_NODE_CONCURRENCY)

        async def run_download(args):
            async with sem:
                return await self._download_file_async(*args)

        for node in nodes:
            fedit_time = node.edit_time
            ftype = node.ftype
            ftoken = node.ftoken
            fname = node.fname
            fcreated_time = node.created_time

            if not self._in_time_range(fedit_time, time_range_start, time_range_end):
                continue

            if ftype == "file":
                file_jobs.append(
                    (
                        len(batch),
                        (
                            fname,
                            ftype,
                            ftoken,
                            fedit_time,
                            fcreated_time,
                            path,
                        ),
                    )
                )
                batch.append(None)
                continue

            if ftype in {"docx", "doc"}:
                response, new_file = await self._export_and_download_async(
                    ftoken,
                    src_type=ftype,
                    file_extension=EXPORT_EXTENSIONS[ftype],
                )
                if not response or not new_file:
                    continue

                fname = (
                    new_file.result.file_name
                    + "."
                    + new_file.result.file_extension
                )

                doc = self._build_document(
                    fname,
                    ftype,
                    new_file.result.file_size,
                    fedit_time,
                    fcreated_time,
                    path,
                    response,
                )
                if doc:
                    batch.append(doc)
                continue

            if ftype == "sheet":
                resp = await self._get_sheet_sub_id_async(ftoken)
                if not resp:
                    continue
                for sheet in resp.sheets:
                    sub_id = sheet.sheet_id
                    resource_type = sheet.resource_type
                    response, new_file = await self._export_and_download_async(
                        ftoken,
                        src_type=ftype,
                        file_extension=EXPORT_EXTENSIONS[resource_type],
                        sub_id=sub_id,
                    )
                    if not response or not new_file:
                        continue

                    fname = (
                        new_file.result.file_name
                        + "."
                        + new_file.result.file_extension
                    )

                    doc = self._build_document(
                        fname,
                        ftype,
                        new_file.result.file_size,
                        fedit_time,
                        fcreated_time,
                        path,
                        response,
                    )
                    if doc:
                        batch.append(doc)
                continue

            if ftype == "bitable":
                resp = await self._get_bitable_sub_id_async(ftoken)
                while resp:
                    for item in resp.items:
                        response, new_file = await self._export_and_download_async(
                            ftoken,
                            src_type=ftype,
                            file_extension=EXPORT_EXTENSIONS[ftype],
                            sub_id=item.table_id,
                        )
                        if not response or not new_file:
                            continue

                        fname = (
                            new_file.result.file_name
                            + "-" + item.name
                            + "."
                            + new_file.result.file_extension
                        )

                        doc = self._build_document(
                            fname,
                            ftype,
                            new_file.result.file_size,
                            fedit_time,
                            fcreated_time,
                            path,
                            response,
                        )
                        if doc:
                            batch.append(doc)
                    if not resp.has_more:
                        break
                    resp = await self._get_bitable_sub_id_async(
                        ftoken,
                        page_token=resp.page_token,
                    )
                continue

            if recurse_hook and ftype == "folder":
                should_recurse, child_nodes, child_path = await recurse_hook(node, path)
                if should_recurse:
                    async for child_batch in self._walk_nodes_async(
                        child_nodes,
                        path=child_path,
                        time_range_start=time_range_start,
                        time_range_end=time_range_end,
                        recurse_hook=recurse_hook,
                    ):
                        yield child_batch
                    continue

        if file_jobs:
            tasks = [asyncio.create_task(run_download(args)) for _, args in file_jobs]
            results = await asyncio.gather(*tasks)
            for (index, _), doc in zip(file_jobs, results):
                batch[index] = doc

        batch = [doc for doc in batch if doc]
        if batch:
            yield batch


    async def _yield_file_recursive_async(
        self,
        folder_token,
        path: str | None = "",
        time_range_start: SecondsSinceUnixEpoch | None = None,
        time_range_end: SecondsSinceUnixEpoch | None = None,
    ):
        if self.token_type == "folder":
            files = await self._list_files_async(folder_token)
            nodes = (self._adapt_drive_node(file) for file in files)
        elif self.token_type == "wiki":
            nodes_list = await self._list_wiki_space_nodes_async(folder_token)
            nodes = (self._adapt_wiki_node(file) for file in nodes_list)
        else:
            nodes = ()

        async def recurse_drive(node: LarkConnector._NormalizedNode, base_path: str | None):
            if node.ftype != "folder":
                return False, (), base_path
            files = await self._list_files_async(node.ftoken)
            child_nodes = (self._adapt_drive_node(file) for file in files)
            child_name = node.fname or ""
            if base_path and child_name:
                child_path = f"{base_path} / {child_name}"
            elif child_name:
                child_path = child_name
            else:
                child_path = base_path
            return True, child_nodes, child_path

        async for batch in self._walk_nodes_async(
            nodes,
            path=path,
            time_range_start=time_range_start,
            time_range_end=time_range_end,
            recurse_hook=recurse_drive,
        ):
            yield batch

    # ---------------------------------
    # Load Files in file manager
    # ---------------------------------
    async def build_tree(self, token, parent_path) -> list[dict]:
        return await self._build_tree_async(token, parent_path)

    async def _build_tree_async(self, token, parent_path) -> list[dict]:
        if self.token_type == "folder":
            files = await self._list_files_async(token)
            nodes = (self._adapt_drive_node(file) for file in files)
        elif self.token_type == "wiki":
            nodes_list = await self._list_wiki_space_nodes_async(token)
            nodes = (self._adapt_wiki_node(file) for file in nodes_list)
        else:
            return []

        batch = []

        for node in nodes:
            if node.ftype not in EXPORT_EXTENSIONS:
                continue

            name = sanitize_filename(node.fname, skip_extension=True)
            item = {
                "name": name,
                "token": node.ftoken,
                "type": node.ftype,
                "name_with_path": parent_path,
                "edit_time": node.edit_time,
                "created_time": node.created_time,
                "children": [], 
            }

            if node.ftype == "folder":
                item["children"] = await self._build_tree_async(
                    node.ftoken,
                    parent_path=parent_path + " / " + name if parent_path else name,
                )

            batch.append(item)

        return batch


    def walk_tree(self, nodes, fn, depth=0):
        for node in nodes:
            fn(node, depth)
            if node.get("children"):
                self.walk_tree(node["children"], fn, depth+1)

        
    def pprint_tree(self, nodes):
        def printer(node, depth):
            indent = "  " * depth
            print(f"{indent}- [{node['type']}] {node['name_with_path']}")

        self.walk_tree(nodes, printer)


    async def load_from_state(self) -> AsyncGenerator[list[Document], None]:
        if not self.client:
            raise ConnectorMissingCredentialError("lark")

        async for batch in self._yield_file_recursive_async(
            folder_token=self.folder_token,
        ):
            yield batch


    async def poll_source(
        self, time_range_start: SecondsSinceUnixEpoch, time_range_end: SecondsSinceUnixEpoch
    ) -> AsyncGenerator[list[Document], None]:
        if not self.client:
            raise ConnectorMissingCredentialError("lark")

        async for batch in self._yield_file_recursive_async(
            folder_token=self.folder_token,
            time_range_start=time_range_start,
            time_range_end=time_range_end,
        ):
            yield batch


    async def _handle_json_node_async(self, node) -> list[Document]:
        fedit_time = node["edit_time"]
        ftype = node["type"]
        ftoken = node["token"]
        fname = node["name"]
        fcreated_time = node["created_time"]
        path = node["name_with_path"]

        if ftype == "file":
            doc = await self._download_file_async(
                fname,
                ftype,
                ftoken,
                fedit_time,
                fcreated_time,
                path,
            )
            return [doc] if doc else []

        if ftype in {"docx", "doc"}:
            response, new_file = await self._export_and_download_async(
                ftoken,
                src_type=ftype,
                file_extension=EXPORT_EXTENSIONS[ftype],
            )
            if not response or not new_file:
                return []

            fname = (
                new_file.result.file_name
                + "."
                + new_file.result.file_extension
            )

            doc = self._build_document(
                fname,
                ftype,
                new_file.result.file_size,
                fedit_time,
                fcreated_time,
                path,
                response,
            )
            return [doc] if doc else []

        if ftype == "sheet":
            resp = await self._get_sheet_sub_id_async(ftoken)
            if not resp:
                return []
            docs: list[Document] = []
            for sheet in resp.sheets:
                sub_id = sheet.sheet_id
                resource_type = sheet.resource_type
                response, new_file = await self._export_and_download_async(
                    ftoken,
                    src_type=ftype,
                    file_extension=EXPORT_EXTENSIONS[resource_type],
                    sub_id=sub_id,
                )
                if not response or not new_file:
                    continue

                fname = (
                    new_file.result.file_name
                    + "."
                    + new_file.result.file_extension
                )

                doc = self._build_document(
                    fname,
                    ftype,
                    new_file.result.file_size,
                    fedit_time,
                    fcreated_time,
                    path,
                    response,
                )
                if doc:
                    docs.append(doc)
            return docs

        if ftype == "bitable":
            docs: list[Document] = []
            resp = await self._get_bitable_sub_id_async(ftoken)
            while resp:
                for item in resp.items:
                    response, new_file = await self._export_and_download_async(
                        ftoken,
                        src_type=ftype,
                        file_extension=EXPORT_EXTENSIONS[ftype],
                        sub_id=item.table_id,
                    )
                    if not response or not new_file:
                        continue

                    fname = (
                        new_file.result.file_name
                        + "-" + item.name
                        + "."
                        + new_file.result.file_extension
                    )

                    doc = self._build_document(
                        fname,
                        ftype,
                        new_file.result.file_size,
                        fedit_time,
                        fcreated_time,
                        path,
                        response,
                    )
                    if doc:
                        docs.append(doc)
                if not resp.has_more:
                    break
                resp = await self._get_bitable_sub_id_async(
                    ftoken,
                    page_token=resp.page_token,
                )
            return docs

        return []

    async def load_from_list(self, token_lst: str):
        sem = asyncio.Semaphore(self._ASYNC_NODE_CONCURRENCY)
        logging.info("inside")
        async def run_node(node):
            async with sem:
                return await self._handle_json_node_async(node)

        results = await asyncio.gather(*(run_node(node) for node in token_lst))
        return [doc for docs in results for doc in docs]

if __name__ == "__main__":
    # folder token: RBGPfNX9Ol7qyIdQFIjco9f8neh
    # wiki token: FWDnwnwqxiRFPLkLPXqcwOkVnUA
    start = time.perf_counter()
    connector = LarkConnector(
        token_type="folder",
        folder_token="RBGPfNX9Ol7qyIdQFIjco9f8neh",
        log_level=lark.LogLevel.WARNING,
    )

    connector.load_credentials(
        {
            "app_id": "cli_a9d757d6fd385cd0",
            "app_secret": "OXGUIrIWZLHLaLHamiyricrnjJJZQGn3",
        }
    )

    async def run_demo():
        tree = await connector.build_tree(connector.folder_token, "")

        print("\n")
        print(tree)
        print("\n")

        connector.pprint_tree(tree)

        token_lst = [
            {
                "name": "(四)就业状况;",
                "token": "GIbod4fESo2C0FxwOsscBSe8nne",
                "type": "docx",
                "name_with_path": "child / grandchild",
                "edit_time": "1768295368",
                "created_time": "1767871915",
                "children": [],
            },
            {
                "name": "员工满意度调研问卷",
                "token": "U0yrbcqsEaawLTsBDKNcXfrvnOf",
                "type": "bitable",
                "name_with_path": "child / grandchild",
                "edit_time": "1767872740",
                "created_time": "1767872737",
                "children": [],
            },
            {
                "name": "quickstart.docx",
                "token": "Exmeb8KIXolcE8x5v3IcYVWYnLh",
                "type": "file",
                "name_with_path": "child / grandchild",
                "edit_time": "1767864856",
                "created_time": "1767864852",
                "children": [],
            },
            {
                "name": "file-sample_100kB.docx",
                "token": "T0bRbqSrFomSqVxz8xncUpsSnPe",
                "type": "file",
                "name_with_path": "child",
                "edit_time": "1767771233",
                "created_time": "1767771230",
                "children": [],
            },
            {
                "name": "我不知道",
                "token": "HiK7bQuTGaQSQHscrPFcW6yHnWC",
                "type": "bitable",
                "name_with_path": "",
                "edit_time": "1768296902",
                "created_time": "1767930222",
                "children": [],
            },
            {
                "name": "项目甘特图 Pro",
                "token": "Mr55sRv9ohm7xLtwSnTcjIG3nVg",
                "type": "sheet",
                "name_with_path": "",
                "edit_time": "1767855515",
                "created_time": "1767855515",
                "children": [],
            },
            {
                "name": "磋商文件.pdf",
                "token": "IH3nbhwNsocHQ4xzHLUcyVDCnZd",
                "type": "file",
                "name_with_path": "",
                "edit_time": "1767771235",
                "created_time": "1767771234",
                "children": [],
            },
            {
                "name": "理想标准-QLiA5500001—2021(V3)汽车禁用_限用物质要求.md",
                "token": "IRl1bHk0CowRJ1xVhshceh4Lnpe",
                "type": "file",
                "name_with_path": "",
                "edit_time": "1767771234",
                "created_time": "1767771234",
                "children": [],
            },
        ]

        docs = await connector.load_from_list(token_lst)
        for doc in docs:
            print(doc.semantic_identifier)

    asyncio.run(run_demo())

    elapsed = time.perf_counter() - start

    print("-" * 100)
    print("\n")
    print("Time count: ", elapsed)
    print("\n")
    print("-" * 100)
