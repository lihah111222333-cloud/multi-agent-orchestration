// =============================================================================
// main.jsx → main.js — React 应用入口点 (无 JSX 版本)
//
// 不使用 JSX 语法, 避免 Wails 资源服务器
// 以 text/plain 返回 .jsx 文件的 MIME 问题。
// =============================================================================

import { createElement } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";

const root = createRoot(document.getElementById("root"));
root.render(createElement(App));
