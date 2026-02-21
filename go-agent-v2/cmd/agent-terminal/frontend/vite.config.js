import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { readFileSync } from "fs";
import { transformSync } from "esbuild";

// 自定义插件: 在 Rollup 解析前将 .js 中的 JSX 转换为纯 JS
// 解决 codex-react 源码使用 .js 扩展名写 JSX 的问题
function jsxInJs() {
    return {
        name: "jsx-in-js",
        enforce: "pre",
        transform(code, id) {
            if (/src\/.*\.js$/.test(id) && !id.includes("node_modules")) {
                const result = transformSync(code, {
                    loader: "jsx",
                    jsx: "automatic",
                    sourcefile: id,
                    sourcemap: true,
                });
                return {
                    code: result.code,
                    map: result.map,
                };
            }
        },
    };
}

// https://vitejs.dev/config/
export default defineConfig({
    plugins: [
        jsxInJs(),
        react(),
    ],

    // Wails 前端: 输出到 dist/, Go embed 打包
    build: {
        outDir: "dist",
        emptyOutDir: true,
    },

    server: {
        port: 5173,
        strictPort: true,
    },
});
