import { defineConfig } from 'vite'

// 前端开发服务器：/api 代理到 Go 后端。
// 后端地址可用环境变量 VITE_PROXY_TARGET 覆盖（默认 http://127.0.0.1:8080）。
// 生产：vite build → dist/{index.html, assets/*}，由 Go 的 web/dist 静态服务直接托管。
export default defineConfig({
  base: '/',
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: process.env.VITE_PROXY_TARGET || 'http://127.0.0.1:8080',
        changeOrigin: true
      }
    }
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsDir: 'assets'
  }
})
