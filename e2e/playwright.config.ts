import { defineConfig } from '@playwright/test'

// WEB_URL 指向被测前端（docker compose 部署或本地 dev server）。
export default defineConfig({
  testDir: './tests',
  timeout: 120_000,
  expect: { timeout: 10_000 },
  workers: 1,
  retries: 0,
  reporter: [['list']],
  use: {
    baseURL: process.env.WEB_URL ?? 'http://localhost:8080',
    trace: 'retain-on-failure',
  },
})
