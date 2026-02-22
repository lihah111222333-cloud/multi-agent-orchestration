# LSP19 语义路由（100条）对比报告

日期：2026-02-23  
数据集：`test-results/router-eval/datasets/lsp19-semantic-complex-100.jsonl`  
评估脚本：`scripts/lsp_router_eval.py`

## 实验设置

- Stage1 路由模型：`qwen2.5:7b`
- Stage2 重排模型：`qwen2.5:14b`
- 工具集：LSP 19 工具
- 样本数：100
- 生成参数：`temperature=0`（脚本内固定）

## 总体结果

| 模式 | Top1 | Hit(primary) | Stage2触发率 | Avg延迟(s) | P95延迟(s) |
|---|---:|---:|---:|---:|---:|
| 单次路由（off） | 0.64 | 0.75 | 0% | 0.6227 | 0.8799 |
| 二次路由（parse_or_ambiguous） | 0.76 | 0.78 | 58% | 1.3530 | 2.5142 |
| 二次路由（parse_or_ambiguous_strict） | 0.69 | 0.75 | 25% | 0.8983 | 1.7428 |

### 关键增益/代价

- `parse_or_ambiguous` 相比单次路由：
  - Top1 `+12` 个点（0.64 -> 0.76）
  - Hit(primary) `+3` 个点（0.75 -> 0.78）
  - Avg 延迟 `+117%`（0.6227s -> 1.3530s）
  - P95 延迟 `+186%`（0.8799s -> 2.5142s）
- `strict` 相比单次路由：
  - Top1 `+5` 个点（0.64 -> 0.69）
  - Hit(primary) `+0`（0.75 -> 0.75）
  - Avg 延迟 `+44%`（0.6227s -> 0.8983s）
  - P95 延迟 `+98%`（0.8799s -> 1.7428s）

## 样本级效果（Top1）

### parse_or_ambiguous

- Stage2 触发：58/100
- 错误修正（stage1错 -> final对）：14
- 反向劣化（stage1对 -> final错）：2
- 触发后净收益：`+12` 条（14 - 2）

### parse_or_ambiguous_strict

- Stage2 触发：25/100
- 错误修正：5
- 反向劣化：0
- 触发后净收益：`+5` 条

## 工具维度观察（Top1）

- 二次路由收益最明显：
  - `lsp_implementation`：`0.167 -> 0.833`（amb），`0.167 -> 0.500`（strict）
  - `lsp_type_definition`：`0.167 -> 0.667`（amb），`0.167 -> 0.500`（strict）
  - `lsp_references`：`0.200 -> 0.600`（amb）
- 二次路由存在回退风险（amb 模式）：
  - `lsp_document_symbol`：`1.000 -> 0.800`
  - `lsp_hover`：`0.800 -> 0.600`

## 生产建议

- 若目标是“语义复杂场景准确率优先”：
  - 先用 `parse_or_ambiguous`，可拿到当前最高 Top1（0.76）。
- 若目标是“延迟与稳定折中”：
  - 用 `parse_or_ambiguous_strict`，延迟更可控，且无反向劣化样本（本批次）。
- 推荐下一步（可直接落地）：
  - 对 `lsp_document_symbol`、`lsp_hover` 增加“高置信不重排”保护；
  - 对 `lsp_implementation`、`lsp_type_definition` 保持高触发重排策略。

## 结果文件

- 单次路由：
  - `test-results/router-eval/2026-02-23-semantic100-baseline-7b/qwen2.5_7b.summary.json`
- 二次路由（ambiguous）：
  - `test-results/router-eval/2026-02-23-semantic100-two-pass-7b14b/qwen2.5_7b__rerank_qwen2.5_14b__parse_or_ambiguous.summary.json`
- 二次路由（strict）：
  - `test-results/router-eval/2026-02-23-semantic100-two-pass-7b14b-strict/qwen2.5_7b__rerank_qwen2.5_14b__parse_or_ambiguous_strict.summary.json`
