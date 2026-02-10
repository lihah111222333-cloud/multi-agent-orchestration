"""Plugin discovery and loading helpers."""

from __future__ import annotations

from ast import Assign, Constant, Name, parse
from hashlib import sha1
from importlib.util import module_from_spec, spec_from_file_location
from pathlib import Path
from types import ModuleType
from typing import Dict, List, Union

PluginPath = Union[str, Path]
_PLUGIN_FILE = "plugin.py"


def _extract_plugin_name(plugin_file: Path) -> str:
    """Extract plugin name from file without importing it."""
    try:
        source = plugin_file.read_text(encoding="utf-8")
    except OSError as exc:
        raise RuntimeError(f"读取插件失败: {plugin_file}") from exc

    try:
        tree = parse(source, filename=str(plugin_file))
    except SyntaxError as exc:
        raise ValueError(f"插件语法错误: {plugin_file}") from exc

    for node in tree.body:
        if not isinstance(node, Assign):
            continue
        if len(node.targets) != 1 or not isinstance(node.targets[0], Name):
            continue
        if node.targets[0].id != "PLUGIN_NAME":
            continue
        if isinstance(node.value, Constant) and isinstance(node.value.value, str):
            plugin_name = node.value.value.strip()
            if plugin_name:
                return plugin_name
            raise ValueError(f"插件名无效: {plugin_file}")
        raise ValueError(f"插件名无效: {plugin_file}")

    return plugin_file.parent.name


def discover_plugins(base_dir: PluginPath) -> List[str]:
    """Discover valid plugin files in direct child directories."""
    root = Path(base_dir)
    if not root.exists():
        return []
    if not root.is_dir():
        raise ValueError(f"base_dir 不是目录: {root}")

    plugin_paths: List[str] = []
    plugin_names: Dict[str, str] = {}

    for child in sorted(root.iterdir(), key=lambda item: item.name):
        if not child.is_dir():
            continue

        plugin_file = child / _PLUGIN_FILE
        if not plugin_file.is_file():
            continue

        plugin_name = _extract_plugin_name(plugin_file)
        previous = plugin_names.get(plugin_name)
        if previous is not None:
            raise ValueError(
                f"重复插件名: {plugin_name}"
                f" (paths: {previous}, {plugin_file})"
            )

        plugin_names[plugin_name] = str(plugin_file)
        plugin_paths.append(str(plugin_file))

    return plugin_paths


def load_plugin_module(path: PluginPath) -> ModuleType:
    """Load a plugin module from a `plugin.py` file path."""
    plugin_file = Path(path)
    if not plugin_file.exists():
        raise FileNotFoundError(f"插件文件不存在: {plugin_file}")
    if not plugin_file.is_file():
        raise ValueError(f"插件路径不是文件: {plugin_file}")

    module_id = sha1(str(plugin_file.resolve()).encode("utf-8")).hexdigest()
    module_name = f"plugins.dynamic_{module_id}"
    spec = spec_from_file_location(module_name, plugin_file)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"无法创建插件加载器: {plugin_file}")

    module = module_from_spec(spec)
    try:
        spec.loader.exec_module(module)
    except Exception as exc:  # noqa: BLE001 - re-wrapped with context
        raise RuntimeError(f"加载插件失败: {plugin_file}") from exc

    return module
