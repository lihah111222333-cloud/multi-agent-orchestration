# ⛔ FROZEN — Web Dashboard 已冻结

**冻结日期**: 2026-02-17
**原因**: Web 面板暂停开发，后续由 Go dashboard 接管

## 冻结范围

| 文件 | 说明 |
|---|---|
| `dashboard.py` | Python HTTP 服务（3920 行） |
| `static/app.js` | 前端 JS（3303 行） |
| `static/style.css` | 前端样式（1840 行） |
| `go-agent-v2/internal/dashboard/` | Go 端 dashboard 包（4 文件） |
| `tests/test_dashboard_*.py` | Dashboard 测试（7 文件） |

## 规则

- **不要修改**上述任何文件
- 新功能请在 Go 项目中实现
- 如需解冻，删除本文件和各文件头部的 FROZEN 标记
