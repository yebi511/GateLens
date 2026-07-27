import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  publicDir: fileURLToPath(new URL('../assets', import.meta.url)),
  build: { emptyOutDir: true },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: process.env.GATELENS_API_PROXY ?? 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
      '/healthz': {
        target: process.env.GATELENS_API_PROXY ?? 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
})
