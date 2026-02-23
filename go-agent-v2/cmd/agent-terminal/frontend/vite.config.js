import { defineConfig } from "vite";

// https://vitejs.dev/config/
export default defineConfig({
    // Wails 前端: 输出到 dist/, Go embed 打包
    build: {
        outDir: "dist",
        emptyOutDir: true,
        rollupOptions: {
            external: ["/wails/runtime.js"],
        },
    },

    server: {
        port: 5173,
        strictPort: true,
    },
});
