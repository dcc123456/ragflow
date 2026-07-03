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
import ast
import os
import importlib
import inspect
from types import ModuleType
from typing import Dict, Type

_package_path = os.path.dirname(__file__)
__all_classes: Dict[str, Type] = {}
__class_modules: dict[str, list[str]] = {}
_loaded_modules: set[str] = set()


def _iter_module_names():
    for filename in os.listdir(_package_path):  # noqa: F821
        if filename.startswith("__") or not filename.endswith(".py") or filename.startswith("base"):
            continue
        yield filename[:-3]


def _load_submodule(module_name: str) -> ModuleType | None:
    try:
        module = importlib.import_module(f".{module_name}", package=__name__)
    except ImportError as e:
        print(f"Warning: Failed to import module {module_name}: {str(e)}")
        _loaded_modules.add(module_name)
        return None

    if module_name not in _loaded_modules:
        _extract_classes_from_module(module)  # noqa: F821
        _loaded_modules.add(module_name)
    globals()[module_name] = module
    return module


def _load_all_submodules() -> None:
    for module_name in _iter_module_names():
        _load_submodule(module_name)


def _get_class_modules() -> dict[str, list[str]]:
    if __class_modules:
        return __class_modules

    for module_name in _iter_module_names():
        module_path = os.path.join(_package_path, f"{module_name}.py")
        try:
            with open(module_path, encoding="utf-8") as f:
                tree = ast.parse(f.read(), filename=module_path)
        except Exception:
            continue

        for node in tree.body:
            if isinstance(node, ast.ClassDef) and not node.name.startswith("_"):
                __class_modules.setdefault(node.name, []).append(module_name)

    return __class_modules


def _extract_classes_from_module(module: ModuleType) -> None:
    for name, obj in inspect.getmembers(module):
        if inspect.isclass(obj) and obj.__module__ == module.__name__ and not name.startswith("_"):
            __all_classes[name] = obj
            globals()[name] = obj


__all__ = list(_get_class_modules()) + ["__all_classes", "component_class"]


def component_class(class_name):
    for module_name in ["agent.component", "agent.tools", "rag.flow"]:
        try:
            return getattr(importlib.import_module(module_name), class_name)
        except Exception:
            # logging.warning(f"Can't import module: {module_name}, error: {e}")
            pass
    assert False, f"Can't import {class_name}"


def __getattr__(name):
    if name in __all_classes:
        return __all_classes[name]

    module_names = set(_iter_module_names())
    if name in module_names:
        module = _load_submodule(name)
        if module is not None:
            return module

    for module_name in _get_class_modules().get(name, []):
        _load_submodule(module_name)
        if name in __all_classes:
            return __all_classes[name]

    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")


def __dir__():
    return sorted(set(globals()) | set(_get_class_modules()))
