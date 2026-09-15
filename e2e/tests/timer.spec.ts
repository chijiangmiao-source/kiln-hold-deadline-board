import { expect, test, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')

/** createTimer 通过真实页面表单创建计时并等待状态面板出现。 */
async function createTimer(page: Page, label: string, minutes: string) {
  await page.goto('/')
  await page.getByLabel('窑位标签').fill(label)
  await page.getByLabel(/保温分钟数/).fill(minutes)
  await page.getByRole('button', { name: '开始保温' }).click()
  await expect(page.getByTestId('timer-status')).toBeVisible()
}

function dockerAvailable(): boolean {
  try {
    execFileSync('docker', ['compose', 'version'], { stdio: 'ignore' })
    return true
  } catch {
    return false
  }
}

test('创建计时后倒计时递减，刷新后仍读取同一不可变截止时刻', async ({ page }) => {
  await createTimer(page, '窑位E2E-1', '5')

  const status = page.getByTestId('status')
  await expect(status).toHaveText('HOLDING')
  const deadline = await status.getAttribute('data-deadline')
  const acceptedAt = await status.getAttribute('data-accepted-at')
  expect(Number(deadline) - Number(acceptedAt)).toBe(5 * 60_000)

  // 倒计时真实递减。
  const first = Number(await page.getByTestId('remaining-ms').textContent())
  await page.waitForTimeout(1200)
  const second = Number(await page.getByTestId('remaining-ms').textContent())
  expect(second).toBeLessThan(first)
  expect(second).toBeGreaterThan(0)

  // 浏览器刷新：仍读取同一不可变截止时刻，不得重新起算。
  await page.reload()
  await expect(page.getByTestId('status')).toHaveAttribute('data-deadline', deadline!)
  await expect(page.getByTestId('status')).toHaveAttribute('data-accepted-at', acceptedAt!)
  await expect(page.getByTestId('status')).toHaveText('HOLDING')
})

test('API 重启后截止时刻不漂移', async ({ page }) => {
  test.skip(!dockerAvailable(), '需要 docker compose 环境')
  await createTimer(page, '窑位E2E-重启', '5')
  const status = page.getByTestId('status')
  const deadline = await status.getAttribute('data-deadline')
  const acceptedAt = await status.getAttribute('data-accepted-at')

  execFileSync('docker', ['compose', 'restart', 'api'], { cwd: repoRoot, stdio: 'inherit' })

  // 等待 API 恢复健康（经前端同源代理探测）。
  await expect
    .poll(
      async () => {
        try {
          const res = await page.request.get('/api/health')
          return res.ok()
        } catch {
          return false
        }
      },
      { timeout: 30_000 },
    )
    .toBe(true)

  await page.reload()
  await expect(page.getByTestId('status')).toHaveAttribute('data-deadline', deadline!)
  await expect(page.getByTestId('status')).toHaveAttribute('data-accepted-at', acceptedAt!)
  await expect(page.getByTestId('status')).toHaveText('HOLDING')
})

test('到达临界毫秒时页面必须切换为 READY 且剩余归零', async ({ page }) => {
  test.setTimeout(120_000)
  await createTimer(page, '窑位E2E-临界', '1')
  const status = page.getByTestId('status')
  await expect(status).toHaveText('HOLDING')
  const deadline = Number(await status.getAttribute('data-deadline'))

  // 接近截止时刻时仍是 HOLDING（不得提前翻转）。
  await page.waitForFunction(
    () => {
      const el = document.querySelector('[data-testid="remaining-ms"]')
      return el !== null && Number(el.textContent) <= 1200
    },
    undefined,
    { timeout: 75_000 },
  )
  await expect(status).toHaveText('HOLDING')

  // 到达临界毫秒后必须翻转为 READY，剩余毫秒归零。
  await expect(status).toHaveText('READY', { timeout: 10_000 })
  const flippedAt = Date.now()
  expect(flippedAt).toBeGreaterThanOrEqual(deadline - 1000)
  await expect(page.getByTestId('remaining')).toHaveText('00:00:00.000')
  await expect(page.getByTestId('remaining-ms')).toHaveText('0')
})

test('表单校验：空标签与越界分钟数被拒绝', async ({ page }) => {
  await page.goto('/')

  // 空标签与纯空白标签。
  await page.getByLabel(/保温分钟数/).fill('5')
  await page.getByRole('button', { name: '开始保温' }).click()
  await expect(page.getByTestId('form-error')).toContainText('窑位标签不能为空')
  await page.getByLabel('窑位标签').fill('   ')
  await page.getByRole('button', { name: '开始保温' }).click()
  await expect(page.getByTestId('form-error')).toContainText('窑位标签不能为空')

  // 分钟数越界与非整数。
  await page.getByLabel('窑位标签').fill('窑位E2E-校验')
  for (const bad of ['0', '181', '1.5']) {
    await page.getByLabel(/保温分钟数/).fill(bad)
    await page.getByRole('button', { name: '开始保温' }).click()
    await expect(page.getByTestId('form-error')).toContainText('1 至 180')
  }

  // 始终未创建计时。
  await expect(page.getByTestId('timer-status')).toHaveCount(0)
})

test('API 直接拒绝非法输入且不给出计时标识', async ({ request }) => {
  const bodies = [
    { label: '', minutes: 5 },
    { label: '   ', minutes: 5 },
    { label: 'x', minutes: 0 },
    { label: 'x', minutes: 181 },
    { label: 'x', minutes: 1.5 },
    { label: 'x', minutes: '5' },
    { label: 'x' },
  ]
  for (const data of bodies) {
    const res = await request.post('/api/timers', { data })
    expect(res.status()).toBe(400)
    const json = await res.json()
    expect(json.error).toBeTruthy()
    expect(json.id).toBeUndefined()
  }
})

test('浏览器时钟偏差超过 30000 毫秒时提示但不改变状态', async ({ page }) => {
  // 把浏览器时钟拨快 45 秒。
  await page.addInitScript(() => {
    const realNow = Date.now
    Date.now = () => realNow() + 45_000
  })
  await createTimer(page, '窑位E2E-偏差', '5')

  await expect(page.getByTestId('skew-warning')).toBeVisible()
  // 状态判定仍以服务端时刻为准，倒计时不受本地时钟影响。
  await expect(page.getByTestId('status')).toHaveText('HOLDING')
  const remaining = Number(await page.getByTestId('remaining-ms').textContent())
  expect(remaining).toBeGreaterThan(280_000)
  expect(remaining).toBeLessThanOrEqual(300_000)
})
