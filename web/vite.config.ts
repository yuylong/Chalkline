import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// 开发期把 /api 与 /mcp 转发到本地 Go 后端（docs/modules/console.md）。
// 生产由 nginx 反代，前端产物只含静态文件。
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/mcp': 'http://localhost:8080',
    },
  },
})
