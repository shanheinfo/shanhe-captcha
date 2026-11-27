import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    lib: {
      entry: fileURLToPath(new URL('./src/index.ts', import.meta.url)),
      name: 'ShanheCaptchaVue',
      fileName: 'shanhe-captcha-vue',
      formats: ['es', 'umd'],
    },
    rollupOptions: {
      // Vue 由调用方提供，不打入构建产物。
      external: ['vue'],
      output: {
        globals: { vue: 'Vue' },
        exports: 'named',
        // 固定 CSS 文件名，便于使用方导入 'shanhe-captcha-vue/style.css'。
        assetFileNames: (asset) =>
          asset.names?.[0]?.endsWith('.css') || asset.name?.endsWith('.css')
            ? 'shanhe-captcha-vue.css'
            : 'assets/[name][extname]',
      },
    },
    cssCodeSplit: false,
    sourcemap: false,
    target: 'es2019',
  },
})
