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
#

"""
Context variables for tracking task and component information across async call chains.

Usage:
    from common.execution_context import task_context, get_log_prefix

    # In component invoke (entry point):
    task_context.set(task_id="abc", component_id="Agent:xxx", component_name="评论清洗")

    # In any downstream code (LLM, embedding, ES, etc.):
    ctx = task_context.get()
    logging.info(f"LLM call: task_id={ctx['task_id']}, component={ctx['component_name']}")

    # Or use the helper:
    logging.info(f"LLM call: {get_log_prefix()}")
"""

from contextvars import ContextVar

task_context: ContextVar[dict] = ContextVar("task_context", default=None)


def set(**kwargs) -> None:
    """Set context values. Call at component entry point."""
    current = task_context.get()
    if current is None:
        current = {}
    current.update(kwargs)
    task_context.set(current)


def get() -> dict:
    """Get current context. Returns empty dict if not set."""
    return task_context.get() or {}


def reset() -> None:
    """Reset context. Call when component finishes."""
    task_context.set(None)


def get_log_prefix() -> str:
    """Returns a string prefix for logging that includes task_id and component info."""
    ctx = get()
    parts = []
    if ctx.get("task_id"):
        parts.append(f"task_id={ctx['task_id']}")
    if ctx.get("component_name"):
        parts.append(f"component={ctx['component_name']}")
    if ctx.get("component_id"):
        parts.append(f"component_id={ctx['component_id']}")
    return ", ".join(parts) if parts else "no_context"
