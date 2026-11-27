import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 演示站独立构建：将 Vue 一并打包，产物可由任意静态服务器托管。
export default defineConfig({
  plugins: [vue()],
  root: 'demo',
  base: '/',
  build: {
    outDir: '../demo-dist',
    emptyOutDir: true,
    target: 'es2019',
  },
  server: {
    port: 5173,
    proxy: {
      '/captcha': 'http://127.0.0.1:8090',
      '/api': 'http://127.0.0.1:8090',
    },
  },
})
