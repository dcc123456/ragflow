#
#  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
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

import asyncio
import importlib
import sys
from contextlib import asynccontextmanager
from types import ModuleType, SimpleNamespace


def _load_mcp_tool_call_conn(monkeypatch):
    for name in [
        "common.mcp_tool_call_conn",
        "mcp",
        "mcp.client",
        "mcp.client.session",
        "mcp.client.sse",
        "mcp.client.streamable_http",
        "mcp.types",
    ]:
        sys.modules.pop(name, None)

    mcp_pkg = ModuleType("mcp")
    mcp_pkg.__path__ = []
    monkeypatch.setitem(sys.modules, "mcp", mcp_pkg)

    mcp_client_pkg = ModuleType("mcp.client")
    mcp_client_pkg.__path__ = []
    monkeypatch.setitem(sys.modules, "mcp.client", mcp_client_pkg)

    session_mod = ModuleType("mcp.client.session")

    class _DummyClientSession:
        async def __aenter__(self):
            return self

        async def __aexit__(self, exc_type, exc, tb):
            return False

        async def initialize(self):
            return None

        async def list_tools(self):
            return SimpleNamespace(tools=[])

        async def call_tool(self, **_kwargs):
            return SimpleNamespace(isError=False, content=[])

    session_mod.ClientSession = _DummyClientSession
    monkeypatch.setitem(sys.modules, "mcp.client.session", session_mod)

    sse_mod = ModuleType("mcp.client.sse")

    @asynccontextmanager
    async def _dummy_sse_client(*_args, **_kwargs):
        yield (object(), object())

    sse_mod.sse_client = _dummy_sse_client
    monkeypatch.setitem(sys.modules, "mcp.client.sse", sse_mod)

    streamable_http_mod = ModuleType("mcp.client.streamable_http")

    @asynccontextmanager
    async def _dummy_streamablehttp_client(*_args, **_kwargs):
        yield (object(), object(), object())

    streamable_http_mod.streamablehttp_client = _dummy_streamablehttp_client
    monkeypatch.setitem(sys.modules, "mcp.client.streamable_http", streamable_http_mod)

    types_mod = ModuleType("mcp.types")
    types_mod.CallToolResult = SimpleNamespace
    types_mod.ListToolsResult = SimpleNamespace

    class _TextContent:
        def __init__(self, text=""):
            self.text = text

    class _Tool:
        def __init__(self, name="", description="", inputSchema=None):
            self.name = name
            self.description = description
            self.inputSchema = inputSchema or {}

    types_mod.TextContent = _TextContent
    types_mod.Tool = _Tool
    monkeypatch.setitem(sys.modules, "mcp.types", types_mod)

    return importlib.import_module("common.mcp_tool_call_conn")


def test_close_sync_closes_event_loop_and_is_idempotent(monkeypatch):
    module = _load_mcp_tool_call_conn(monkeypatch)
    started = asyncio.Event()

    async def _fake_mcp_server_loop(self):
        started.set()
        await asyncio.Event().wait()

    monkeypatch.setattr(module.MCPToolCallSession, "_mcp_server_loop", _fake_mcp_server_loop)
    server = SimpleNamespace(id="srv-1", url="http://example.com", headers={}, server_type=module.MCPServerType.SSE)

    session = module.MCPToolCallSession(server)
    asyncio.run(asyncio.wait_for(started.wait(), timeout=1))

    loop = session._event_loop
    thread = session._loop_thread

    session.close_sync(timeout=1)

    assert session._close is True
    assert session._closed_event.is_set()
    assert not thread.is_alive()
    assert loop.is_closed()
    assert session not in module.MCPToolCallSession._ALL_INSTANCES

    session.close_sync(timeout=1)

    assert session._closed_event.is_set()
    assert loop.is_closed()


def test_close_multiple_sessions_closes_real_mcp_session(monkeypatch):
    module = _load_mcp_tool_call_conn(monkeypatch)
    started = asyncio.Event()

    async def _fake_mcp_server_loop(self):
        started.set()
        await asyncio.Event().wait()

    monkeypatch.setattr(module.MCPToolCallSession, "_mcp_server_loop", _fake_mcp_server_loop)
    server = SimpleNamespace(id="srv-2", url="http://example.com", headers={}, server_type=module.MCPServerType.SSE)

    session = module.MCPToolCallSession(server)
    asyncio.run(asyncio.wait_for(started.wait(), timeout=1))

    loop = session._event_loop
    thread = session._loop_thread

    module.close_multiple_mcp_toolcall_sessions([session])

    assert session._close is True
    assert session._closed_event.is_set()
    assert not thread.is_alive()
    assert loop.is_closed()
    assert session not in module.MCPToolCallSession._ALL_INSTANCES


def test_close_multiple_sessions_invokes_each_closer(monkeypatch):
    module = _load_mcp_tool_call_conn(monkeypatch)

    class _DummySession:
        def __init__(self):
            self.closed = False

        async def close(self):
            self.closed = True

    sessions = [_DummySession(), _DummySession()]

    module.close_multiple_mcp_toolcall_sessions(sessions)

    assert all(session.closed for session in sessions)
