import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    // 监听所有网卡，同一局域网的手机可通过 http://<电脑IP>:5173 访问
    host: true,
    // 开发时把 /api 转发给 Go 服务，前后端同源，Cookie 正常工作
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
