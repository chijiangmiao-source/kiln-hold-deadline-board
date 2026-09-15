import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// 本地开发时把 /api 代理到 Go API（默认 8080，可用 API_PORT 覆盖）。
const apiTarget = `http://localhost:${process.env.API_PORT ?? '8080'}`

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: apiTarget, changeOrigin: true },
    },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
})
