import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// dev 期前端流量走 Vite dev server，/api 代理到后端（design D7）
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
})
