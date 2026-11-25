import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

// 开发模式通过代理访问后端,规避 CORS;生产模式前端由后端嵌入式提供
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://localhost:9900", changeOrigin: true },
      "/mock": { target: "http://localhost:9901", changeOrigin: true },
    },
  },
  build: { outDir: "dist", chunkSizeWarningLimit: 1500 },
});
