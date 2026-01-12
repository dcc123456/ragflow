import logging
from dataclasses import dataclass
from typing import Any, Callable, Generator, Iterable
import hashlib
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
from common.data_source.models import Document, SecondsSinceUnixEpoch, GenerateDocumentsOutput
from common.data_source.config import DocumentSource
from common.data_source.utils import get_file_ext, sanitize_filename

EXPORT_EXTENSIONS = {
    "doc": "docx",
    "docx": "docx",
    "sheet": "csv",
    "bitable": "csv",
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
        raw: Any

    def __init__(self, token_type, log_level = lark.LogLevel.ERROR) -> None:
        self.client: lark.Client | None = None
        self.app_id: str | None = None
        self.app_secret: str | None = None
        self.token_type = token_type
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


    def _debug_write_file(self, file_name: str, response) -> None:
        # if self.log_level != lark.LogLevel.DEBUG:
        #     return
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


    def _list_files(self, folder_token, page_size: int | None = None) -> list[Any]:
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

            response: ListFileResponse = self.client.drive.v1.file.list(request)

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

    @staticmethod


    def _parse_content_length(headers: dict[str, Any] | None) -> int | None:
        if not headers:
            return None
        content_length = headers.get("Content-Length")
        try:
            return int(content_length) if content_length is not None else None
        except (TypeError, ValueError):
            return None


    def _build_document(
        self,
        fname,
        ftype,
        ftoken,
        fmodified_time,
        fcreated_time,
        folder_path,
        response,
        new_file=None,
        bitable_item_name="",
    ) -> Document:
        base_name = fname or ftoken
        file_name = sanitize_filename(
            (
                new_file.result.file_name
                + ("-" + bitable_item_name if bitable_item_name else "")
                + "."
                + new_file.result.file_extension
            )
            if new_file
            else base_name
        )
        title = f"{folder_path} / {file_name}" if folder_path else file_name
        headers = response.raw.headers if response and response.raw else None
        size_bytes = self._parse_content_length(headers)
        extension = new_file.result.file_extension if new_file else get_file_ext(file_name)
        # Only if debug mode
        self._debug_write_file(file_name, response)


        doc_id = hashlib.sha256(f"{title}{ftype}".encode("utf-8")).hexdigest()

        return Document(
            id=f"lark:{doc_id}",
            blob=response.file.read(),
            source=DocumentSource.LARK,
            semantic_identifier=title,
            extension=extension,
            doc_updated_at=fmodified_time,
            size_bytes=new_file.result.file_size if new_file else size_bytes,
            metadata={"created_at": fcreated_time},
        )

    def _adapt_drive_node(self, file) -> _NormalizedNode:
        return self._NormalizedNode(
            edit_time=file.modified_time,
            ftype=file.type,
            ftoken=file.token,
            fname=file.name,
            created_time=file.created_time,
            raw=file,
        )

    def _adapt_wiki_node(self, file) -> _NormalizedNode:
        return self._NormalizedNode(
            edit_time=file.obj_edit_time,
            ftype=file.obj_type,
            ftoken=file.obj_token,
            fname=getattr(file, "title", None) or getattr(file, "name", None),
            created_time=getattr(file, "obj_create_time", None),
            raw=file,
        )


    def _download_file(
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
        response: DownloadFileResponse = self.client.drive.v1.file.download(request)
        if not response.success():
            lark.logger.error(
                f"client.drive.v1.file.download failed, code: {response.code}")
            return None

        return self._build_document(
            fname,
            ftype,
            ftoken,
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

        response: CreateExportTaskResponse = self.client.drive.v1.export_task.create(request)

        if not response.success():
            lark.logger.error(
                f"client.drive.v1.export_task.create failed, "
                f"code: {response.code}, "
                f"msg: {response.msg}, "
                f"log_id: {response.get_log_id()}, "
            )
            return None

        return response.data.ticket


    def _download_ticket(self, new_file):
        request: DownloadExportTaskRequest = (
            DownloadExportTaskRequest.builder()
            .file_token(new_file.result.file_token)
            .build()
        )

        response: DownloadExportTaskResponse = self.client.drive.v1.export_task.download(request)

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
        timeout_seconds: int = 30,
        poll_interval_seconds: float = 1.0,
    ):
        request: GetExportTaskRequest = (
            GetExportTaskRequest.builder()
            .ticket(ticket)
            .token(file_token)
            .build()
        )
        deadline = time.time() + timeout_seconds
        last_status = None

        while True:
            response: GetExportTaskResponse = self.client.drive.v1.export_task.get(request)

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
            file_token_result = getattr(result, "file_token", None) if result else None
            last_status = job_status

            if file_token_result:
                lark.logger.info(lark.JSON.marshal(data, indent=4))
                return data

            if time.time() >= deadline:
                lark.logger.error(
                    "client.drive.v1.export_task.get timed out, "
                    f"ticket: {ticket}, "
                    f"file_token: {file_token}, "
                    f"last_status: {last_status}"
                )
                return None

            time.sleep(poll_interval_seconds)


    def _get_sheet_sub_id(self, spreadsheet_token: str) -> str | None:
        request: QuerySpreadsheetSheetRequest = (
            QuerySpreadsheetSheetRequest.builder()
            .spreadsheet_token(spreadsheet_token)
            .build()
        )

        response = self.client.sheets.v3.spreadsheet_sheet.query(request)

        if not response.success():
            lark.logger.error(
                "client.sheets.v3.spreadsheet_sheet.query failed, "
                f"code: {response.code}, "
                f"msg: {response.msg}, "
                f"log_id: {response.get_log_id()}, "
            )
            return None

        return response.data


    def _get_bitable_sub_id(
        self, app_token: str, page_size: int | None = None, page_token: str | None = None
    ) -> str | None:
        body = ListAppTableRequest.builder().app_token(app_token)

        if page_size is not None:
            body = body.page_size(page_size)
        if page_token:
            body = body.page_token(page_token)

        request: ListAppTableRequest = body.build()

        response: ListAppTableResponse = self.client.bitable.v1.app_table.list(request)

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


    def _list_wiki_space_nodes(
        self,
        space_token: str,
        page_size: int | None = None,
    ) -> list[Any]:
        if not space_token:
            logging.info("No wiki space_token provided")
            return []


        space_id = self._get_wiki_space_id(space_token).node.space_id

        nodes: list[Any] = []
        page_token: str | None = None

        while True:
            body = ListSpaceNodeRequest.builder().space_id(space_id)
            if page_size is not None:
                body = body.page_size(page_size)
            if page_token:
                body = body.page_token(page_token)
            request: ListSpaceNodeRequest = body.build()

            response = self.client.wiki.v2.space_node.list(request)

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


    def _export_and_download(
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
            file_extension=file_extension,
            sub_id=sub_id,
        )

        if not ticket:
            return None, None

        new_file = self._get_export_task_ticket(ticket, token)
        if not new_file:
            return None, None

        response = self._download_ticket(new_file)
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

    def _walk_nodes(
        self,
        nodes: Iterable[_NormalizedNode],
        *,
        path: str | None = "",
        time_range_start: SecondsSinceUnixEpoch | None = None,
        time_range_end: SecondsSinceUnixEpoch | None = None,
        recurse_hook: Callable[[_NormalizedNode, str | None], tuple[bool, Iterable[_NormalizedNode], str | None]]
        | None = None,
    ) -> GenerateDocumentsOutput:
        batch: list[Document] = []

        for node in nodes:
            edit_time = node.edit_time
            ftype = node.ftype
            ftoken = node.ftoken
            fname = node.fname
            fcreated_time = node.created_time

            if not self._in_time_range(edit_time, time_range_start, time_range_end):
                continue

            if ftype == "file":
                batch.append(
                    self._download_file(
                        fname,
                        ftype,
                        ftoken,
                        edit_time,
                        fcreated_time,
                        path,
                    )
                )
                continue

            if ftype in {"docx", "doc"}:
                response, new_file = self._export_and_download(
                    ftoken,
                    src_type=ftype,
                    file_extension=EXPORT_EXTENSIONS[ftype],
                )
                if not response or not new_file:
                    continue
                doc = self._build_document(
                    fname,
                    ftype,
                    ftoken,
                    edit_time,
                    fcreated_time,
                    path,
                    response,
                    new_file,
                )
                if doc:
                    batch.append(doc)
                continue

            if ftype == "sheet":
                resp = self._get_sheet_sub_id(ftoken)
                for sheet in resp.sheets:
                    sub_id = sheet.sheet_id
                    resource_type = sheet.resource_type
                    response, new_file = self._export_and_download(
                        ftoken,
                        src_type=ftype,
                        file_extension=EXPORT_EXTENSIONS[resource_type],
                        sub_id=sub_id,
                    )
                    if not response or not new_file:
                        continue
                    doc = self._build_document(
                        fname,
                        ftype,
                        ftoken,
                        edit_time,
                        fcreated_time,
                        path,
                        response,
                        new_file,
                    )
                    if doc:
                        batch.append(doc)
                continue

            if ftype == "bitable":
                resp = self._get_bitable_sub_id(ftoken)
                while True:
                    for item in resp.items:
                        response, new_file = self._export_and_download(
                            ftoken,
                            src_type=ftype,
                            file_extension=EXPORT_EXTENSIONS[ftype],
                            sub_id=item.table_id,
                        )
                        if not response or not new_file:
                            continue
                        doc = self._build_document(
                            fname,
                            ftype,
                            ftoken,
                            edit_time,
                            fcreated_time,
                            path,
                            response,
                            new_file,
                            item.name,
                        )
                        if doc:
                            batch.append(doc)
                    if not resp.has_more:
                        break
                    resp = self._get_bitable_sub_id(
                        ftoken,
                        page_token=resp.page_token,
                    )
                continue

            if recurse_hook:
                should_recurse, child_nodes, child_path = recurse_hook(node, path)
                if should_recurse:
                    yield from self._walk_nodes(
                        child_nodes,
                        path=child_path,
                        time_range_start=time_range_start,
                        time_range_end=time_range_end,
                        recurse_hook=recurse_hook,
                    )
                    continue

        if batch:
            yield batch


    def _yield_file_recursive(
        self,
        folder_token,
        path: str | None = "",
        time_range_start: SecondsSinceUnixEpoch | None = None,
        time_range_end: SecondsSinceUnixEpoch | None = None,
    ) -> GenerateDocumentsOutput:
        nodes = (self._adapt_drive_node(file) for file in self._list_files(folder_token))

        def recurse_drive(node: LarkConnector._NormalizedNode, base_path: str | None):
            if node.ftype != "folder":
                return False, (), base_path
            child_nodes = (self._adapt_drive_node(file) for file in self._list_files(node.ftoken))
            child_name = node.fname or ""
            if base_path and child_name:
                child_path = f"{base_path} / {child_name}"
            elif child_name:
                child_path = child_name
            else:
                child_path = base_path
            return True, child_nodes, child_path

        yield from self._walk_nodes(
            nodes,
            path=path,
            time_range_start=time_range_start,
            time_range_end=time_range_end,
            recurse_hook=recurse_drive,
        )


    def _yield_wiki_nodes_recursive(
        self,
        space_id,
        path: str | None = "",
        time_range_start: SecondsSinceUnixEpoch | None = None,
        time_range_end: SecondsSinceUnixEpoch | None = None,
    ) -> GenerateDocumentsOutput:
        nodes = (self._adapt_wiki_node(file) for file in self._list_wiki_space_nodes(space_id))
        yield from self._walk_nodes(
            nodes,
            path=path,
            time_range_start=time_range_start,
            time_range_end=time_range_end,
        )


    def load_from_state(self, token) -> Generator[list[Document], None, None]:
        if not self.client:
            raise ConnectorMissingCredentialError("lark")
        
        self.folder_token = token

        if self.token_type == "wiki":
            return self._yield_wiki_nodes_recursive(
                space_id=token,
            )
        
        elif self.token_type == "folder":
            return self._yield_file_recursive(
                folder_token=token, 
                )


    def poll_source(
        self, token, time_range_start: SecondsSinceUnixEpoch, time_range_end: SecondsSinceUnixEpoch
    ) -> Generator[list[Document], None, None]:
        if not self.client:
            raise ConnectorMissingCredentialError("lark")

        self.folder_token = token

        if self.token_type == "wiki":
            return self._yield_wiki_nodes_recursive(
                space_id=token,
                time_range_start=time_range_start,
                time_range_end=time_range_end,
            )
        elif self.token_type == "folder":
            return self._yield_file_recursive(
                folder_token=token,
                time_range_start=time_range_start,
                time_range_end=time_range_end,
            )

if __name__ == "__main__":
    # folder token: RBGPfNX9Ol7qyIdQFIjco9f8neh
    # wiki token: FWDnwnwqxiRFPLkLPXqcwOkVnUA

    connector = LarkConnector(
        token_type="folder", 
        log_level=lark.LogLevel.DEBUG
        )
    
    connector.load_credentials(
        {
            "app_id": "cli_a9d757d6fd385cd0",
            "app_secret": "OXGUIrIWZLHLaLHamiyricrnjJJZQGn3",
        }
    )
    connector.validate_connector_settings()
    docs = connector.load_from_state("RBGPfNX9Ol7qyIdQFIjco9f8neh")
    for doc in docs:
        for f in doc:
            print(f.semantic_identifier)
