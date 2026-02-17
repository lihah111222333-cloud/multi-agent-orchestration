"""Plugin discovery and loading helpers."""

from __future__ import annotations

from ast import Assign, Constant, Name, parse
from hashlib import sha1
from importlib.util import module_from_spec, spec_from_file_location
from pathlib import Path
from types import ModuleType
from typing import Dict, List, Sequence, Union

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


def discover_plugin_index(base_dir: PluginPath) -> Dict[str, Path]:
    """Discover plugin directories and return `{plugin_name: plugin_file}` mapping."""
    root = Path(base_dir)
    if not root.exists():
        return {}
    if not root.is_dir():
        raise ValueError(f"base_dir 不是目录: {root}")

    plugin_index: Dict[str, Path] = {}
    for child in sorted(root.iterdir(), key=lambda item: item.name):
        if not child.is_dir():
            continue
        plugin_file = child / _PLUGIN_FILE
        if not plugin_file.is_file():
            continue

        plugin_name = _extract_plugin_name(plugin_file)
        previous = plugin_index.get(plugin_name)
        if previous is not None:
            raise ValueError(
                f"重复插件名: {plugin_name}"
                f" (paths: {previous}, {plugin_file})"
            )
        plugin_index[plugin_name] = plugin_file

    return plugin_index


def discover_plugins(base_dir: PluginPath) -> List[str]:
    """Return all discovered `plugin.py` paths."""
    plugin_index = discover_plugin_index(base_dir)
    return [str(plugin_file) for plugin_file in plugin_index.values()]


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


def load_plugins(plugin_names: Sequence[str], base_dir: PluginPath) -> Dict[str, ModuleType]:
    """Load plugin modules by declared names.

    Each module must expose:
    - `PLUGIN_NAME: str`
    - `PLUGIN_TOOLS: tuple|list`
    """

    requested_names: List[str] = []
    seen_names: set[str] = set()
    for raw_name in plugin_names:
        plugin_name = str(raw_name or "").strip()
        if not plugin_name:
            raise ValueError("插件名不能为空")
        if plugin_name in seen_names:
            raise ValueError(f"重复插件声明: {plugin_name}")
        seen_names.add(plugin_name)
        requested_names.append(plugin_name)

    plugin_index = discover_plugin_index(base_dir)
    loaded: Dict[str, ModuleType] = {}
    for plugin_name in requested_names:
        plugin_file = plugin_index.get(plugin_name)
        if plugin_file is None:
            raise KeyError(f"插件不存在: {plugin_name}")

        module = load_plugin_module(plugin_file)
        actual_name = str(getattr(module, "PLUGIN_NAME", "")).strip()
        if actual_name != plugin_name:
            raise ValueError(
                f"插件名不匹配: requested={plugin_name}, "
                f"actual={actual_name or '<empty>'}, path={plugin_file}"
            )

        if not hasattr(module, "PLUGIN_TOOLS"):
            raise ValueError(f"插件缺少 PLUGIN_TOOLS: {plugin_name}")
        plugin_tools = getattr(module, "PLUGIN_TOOLS")
        if not isinstance(plugin_tools, (list, tuple)):
            raise TypeError(f"PLUGIN_TOOLS 类型非法: {plugin_name}")

        loaded[plugin_name] = module

    return loaded
