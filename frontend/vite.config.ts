import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// dev 期前端流量走 Vite dev server，/api 代理到后端（design D7）
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
    // 浏览器回归在测试开始时才写入 .*-qa.html 夹具；冷启动时监听到这次写入
    // 会把正在测的页面整页刷新，测试随之丢掉状态。夹具照常可访问，只是不监听。
    watch: {
      ignored: ['**/.*-qa.html'],
    },
  },
})
