import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// /api 代理目标：compose 容器内为 http://api:8080（API_TARGET），
// 本地开发默认 http://localhost:8080（可用 API_PORT 覆盖）。
const apiTarget =
  process.env.API_TARGET ?? `http://localhost:${process.env.API_PORT ?? '8080'}`

const proxy = { '/api': { target: apiTarget, changeOrigin: true } }

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy,
  },
  // 前端容器以 vite preview 托管构建产物，同样代理 /api。
  preview: {
    port: 5173,
    proxy,
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
})
