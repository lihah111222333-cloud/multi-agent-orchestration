"""Agent 工厂与规格定义"""

from __future__ import annotations

import inspect
from dataclasses import dataclass
from typing import TYPE_CHECKING, Any, Callable, Dict, Mapping, Optional, Sequence, Tuple

from agents.runtime_control import run_in_agent_thread

if TYPE_CHECKING:
    from mcp.server.fastmcp import FastMCP


class _MissingDefault:
    pass


MISSING = _MissingDefault()


@dataclass(frozen=True)
class ToolParam:
    name: str
    annotation: type = str
    default: Any = MISSING


@dataclass(frozen=True)
class ToolSpec:
    name: str
    description: str
    params: Tuple[ToolParam, ...]
    response_template: Optional[str] = None
    response_builder: Optional[Callable[[Mapping[str, Any]], str]] = None

    def render(self, values: Mapping[str, Any]) -> str:
        if self.response_builder is not None:
            return self.response_builder(values)
        if self.response_template is None:
            raise ValueError(f"ToolSpec[{self.name}] 缺少响应模板")
        # Use format_map with a safe wrapper to prevent attribute-access injection
        # (e.g. {__class__} in user-supplied values)
        return self.response_template.format_map(_SafeFormatDict(values))


class _SafeFormatDict(dict):
    """Dict wrapper that blocks attribute access via format strings."""

    def __init__(self, data: Mapping[str, Any]) -> None:
        super().__init__(data)

    def __getattr__(self, name: str) -> str:
        raise KeyError(name)


@dataclass(frozen=True)
class AgentSpec:
    key: str
    server_name: str
    description: str
    tools: Tuple[ToolSpec, ...]


def _build_signature(params: Sequence[ToolParam]) -> inspect.Signature:
    signature_params = []
    for param in params:
        default = inspect.Parameter.empty if param.default is MISSING else param.default
        signature_params.append(
            inspect.Parameter(
                name=param.name,
                kind=inspect.Parameter.POSITIONAL_OR_KEYWORD,
                default=default,
                annotation=param.annotation,
            )
        )
    return inspect.Signature(parameters=signature_params, return_annotation=str)


def build_tool_callable(spec: ToolSpec) -> Callable[..., str]:
    signature = _build_signature(spec.params)
    annotations: Dict[str, Any] = {p.name: p.annotation for p in spec.params}
    annotations["return"] = str

    def generated_tool(*args: Any, **kwargs: Any) -> str:
        bound = signature.bind(*args, **kwargs)
        bound.apply_defaults()
        values = dict(bound.arguments)
        return run_in_agent_thread(lambda: spec.render(values))

    generated_tool.__name__ = spec.name
    generated_tool.__doc__ = spec.description
    generated_tool.__signature__ = signature
    generated_tool.__annotations__ = annotations
    return generated_tool


def create_server_from_spec(spec: AgentSpec) -> "FastMCP":
    from agents.base_agent import create_agent_server

    server = create_agent_server(spec.server_name, spec.description)
    for tool in spec.tools:
        server.tool()(build_tool_callable(tool))
    return server


def _default_dynamic_spec(agent_key: str, agent_name: str = "") -> AgentSpec:
    display_name = agent_name or agent_key
    return AgentSpec(
        key=agent_key,
        server_name=agent_key.replace("_", "-"),
        description=f"动态 Agent: {display_name}",
        tools=(
            ToolSpec(
                name="execute_task",
                description="执行通用任务",
                params=(
                    ToolParam("task", str),
                    ToolParam("context", str, ""),
                ),
                response_builder=lambda values: (
                    f"[{agent_key}] 已处理任务: {values['task']}"
                    + (f" | context={values['context']}" if values.get("context") else "")
                ),
            ),
            ToolSpec(
                name="report_status",
                description="汇报 Agent 当前状态",
                params=(ToolParam("status", str, "ok"),),
                response_builder=lambda values: f"[{agent_key}] 状态: {values['status']}",
            ),
        ),
    )


def get_agent_spec_by_key(agent_key: str, agent_name: str = "") -> AgentSpec:
    from agents.specs import AGENT_SPECS

    spec = AGENT_SPECS.get(agent_key)
    if spec is not None:
        return spec
    return _default_dynamic_spec(agent_key, agent_name=agent_name)


def run_agent_by_key(agent_key: str, agent_name: str = "") -> None:
    from agents.base_agent import run_agent
    from agents.runtime_control import initialize_agent_runtime

    initialize_agent_runtime(agent_key)
    spec = get_agent_spec_by_key(agent_key, agent_name=agent_name)
    server = create_server_from_spec(spec)
    run_agent(server)
