# LSP 19 工具路由评测（100 条）

- 时间: 2026-02-23
- 数据集: `dataset.jsonl`（100 条，覆盖 19 工具）
- 模型: `qwen2.5:7b`, `qwen2.5:14b`
- 评测脚本: `scripts/lsp_router_eval.py`

## 总体结果

| model | top1_accuracy | hit_primary_accuracy | exact_set_accuracy | parse_fail_rate | p95 latency |
|---|---:|---:|---:|---:|---:|
| qwen2.5:7b | 0.84 | 0.92 | 0.78 | 0.01 | 0.722s |
| qwen2.5:14b | 0.84 | 0.86 | 0.76 | 0.07 | 1.318s |

## 关键观察

1. 两个模型 top1 同为 84%，但 7B 在 hit/解析稳定性/延迟上更好。
2. 14B 的主要问题是别名输出（如 `lsp_typeDefinition`）导致解析失败率偏高（7%）。
3. 共同薄弱区在语义相近工具：`implementation` / `type_definition` / `type_hierarchy`。

## 按工具薄弱点（节选）

- qwen2.5:7b
  - `lsp_implementation`: top1=0.3333
  - `lsp_type_definition`: top1=0.5
  - `lsp_code_action`: top1=0.5
- qwen2.5:14b
  - `lsp_type_definition`: top1=0.0
  - `lsp_type_hierarchy`: top1=0.2
  - `lsp_implementation`: top1=0.5

## 产物

- `aggregate.json`
- `qwen2.5_7b.summary.json`
- `qwen2.5_7b.details.jsonl`
- `qwen2.5_14b.summary.json`
- `qwen2.5_14b.details.jsonl`
