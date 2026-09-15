import { expect, test } from '@playwright/test'

/** 看板相关用例的专属标签前缀，避免与共享数据库中其他用例的记录混淆。 */
const MINE = '窑位E2E-看板-'

/** createViaApi 直接经 API 创建计时，返回服务端响应体。 */
async function createViaApi(
  request: import('@playwright/test').APIRequestContext,
  label: string,
  minutes: number,
) {
  const res = await request.post('/api/timers', { data: { label, minutes } })
  expect(res.status()).toBe(201)
  return (await res.json()) as {
    id: number
    label: string
    minutes: number
    accepted_at: number
    deadline: number
    now: number
    status: string
    remaining_ms: number
  }
}

test('看板按保温中→已到时、截止时刻升序展示，点选后刷新仍是同一计时', async ({
  page,
  request,
}) => {
  test.setTimeout(150_000)
  // 标签带本次运行唯一前缀：共享数据库中的历史记录不影响断言。
  const RUN = `${MINE}${Date.now()}-`
  // 乱序创建：短（约 60 秒后到时）、长、中——创建顺序与看板顺序不同。
  const short = await createViaApi(request, `${RUN}短`, 1)
  const long = await createViaApi(request, `${RUN}长`, 180)
  const mid = await createViaApi(request, `${RUN}中`, 90)
  expect(short.id).toBeLessThan(long.id)
  expect(long.id).toBeLessThan(mid.id)

  await page.goto('/')
  await page.getByTestId('open-board').click()
  await expect(page.getByTestId('timer-board')).toBeVisible()

  // 只关注本用例创建的记录，相对顺序不受共享数据库中其他记录影响。
  const myLabels = page.getByTestId('board-item-label').filter({ hasText: RUN })

  // 全部保温中：按截止时刻升序 → 短、中、长。
  await expect(myLabels).toHaveText([`${RUN}短`, `${RUN}中`, `${RUN}长`])

  // 卡片倒计时沿用校准时钟真实递减。
  const firstRemaining = await page
    .getByTestId('board-item-remaining')
    .first()
    .textContent()
  await page.waitForTimeout(1200)
  const secondRemaining = await page
    .getByTestId('board-item-remaining')
    .first()
    .textContent()
  expect(secondRemaining).not.toBe(firstRemaining)

  // 短计时到点后：已到时记录排到保温中之后（下一次轮询拉取后生效）。
  await expect(myLabels).toHaveText([`${RUN}中`, `${RUN}长`, `${RUN}短`], {
    timeout: 90_000,
  })

  // 点选任一记录进入原有详情，id 写入原 localStorage 键。
  await page
    .getByTestId('board-item')
    .filter({ hasText: `${RUN}长` })
    .click()
  await expect(page.getByTestId('timer-status')).toBeVisible()
  await expect(page.getByTestId('status')).toHaveAttribute('data-deadline', String(long.deadline))
  await expect(page.getByTestId('status')).toHaveAttribute(
    'data-accepted-at',
    String(long.accepted_at),
  )
  expect(await page.evaluate(() => localStorage.getItem('kiln.timerId'))).toBe(String(long.id))

  // 刷新后仍落到同一不可变计时。
  await page.reload()
  await expect(page.getByTestId('timer-status')).toBeVisible()
  await expect(page.getByTestId('timer-status')).toContainText(`${RUN}长`)
  await expect(page.getByTestId('status')).toHaveAttribute('data-deadline', String(long.deadline))
  await expect(page.getByTestId('status')).toHaveText('HOLDING')
})

test('看板拉取失败只在看板区域提示并按既有轮询自动恢复', async ({ page }) => {
  // 标签带本次运行唯一后缀，避免与共享数据库中的历史记录混淆。
  const label = `窑位E2E-故障恢复-${Date.now()}`
  // 先通过表单创建并选中一个计时。
  await page.goto('/')
  await page.getByLabel('窑位标签').fill(label)
  await page.getByLabel(/保温分钟数/).fill('5')
  await page.getByRole('button', { name: '开始保温' }).click()
  await expect(page.getByTestId('timer-status')).toBeVisible()
  const deadline = await page.getByTestId('status').getAttribute('data-deadline')

  // 看板正常加载。
  await page.getByTestId('open-board').click()
  await expect(page.getByTestId('board-item').filter({ hasText: label })).toBeVisible()

  // 模拟看板接口故障：下一个轮询周期后只在看板区域提示。
  await page.route(/\/api\/timers\?view=board/, (route) => route.abort())
  await expect(page.getByTestId('board-error')).toBeVisible({ timeout: 15_000 })

  // 已选计时未被清除，返回详情仍是同一计时。
  expect(await page.evaluate(() => localStorage.getItem('kiln.timerId'))).not.toBeNull()
  await page.getByTestId('board-back').click()
  await expect(page.getByTestId('timer-status')).toBeVisible()
  await expect(page.getByTestId('status')).toHaveAttribute('data-deadline', deadline!)

  // 故障恢复后按既有轮询自动重载，提示消失。
  await page.getByTestId('open-board').click()
  await page.unroute(/\/api\/timers\?view=board/)
  await expect(page.getByTestId('board-error')).toHaveCount(0, { timeout: 15_000 })
  await expect(page.getByTestId('board-item').filter({ hasText: label })).toBeVisible()
})

test('看板故障不覆盖表单上的创建错误', async ({ page }) => {
  await page.goto('/')

  // 创建接口故障：表单显示服务端创建错误。
  await page.route(/\/api\/timers$/, (route) => {
    if (route.request().method() === 'POST') {
      return route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'failed to persist timer' }),
      })
    }
    return route.continue()
  })
  await page.getByLabel('窑位标签').fill('窑位E2E-创建故障')
  await page.getByLabel(/保温分钟数/).fill('5')
  await page.getByRole('button', { name: '开始保温' }).click()
  await expect(page.getByTestId('form-error')).toHaveText('failed to persist timer')

  // 看板也故障：错误只出现在看板区域。
  await page.route(/\/api\/timers\?view=board/, (route) => route.abort())
  await page.getByTestId('open-board').click()
  await expect(page.getByTestId('board-error')).toBeVisible()

  // 返回表单：创建错误原样保留，未被看板故障覆盖。
  await page.getByTestId('board-back').click()
  await expect(page.getByTestId('form-error')).toHaveText('failed to persist timer')
})

test('旧版列表、创建与单条读取契约不受影响', async ({ request }) => {
  // 先创建截止更晚的记录，再创建截止更早的记录。
  const a = await createViaApi(request, '窑位E2E-契约-甲', 180)
  const b = await createViaApi(request, '窑位E2E-契约-乙', 1)

  // 创建响应结构不变。
  for (const created of [a, b]) {
    expect(Object.keys(created).sort()).toEqual(
      ['id', 'label', 'minutes', 'accepted_at', 'deadline', 'now', 'status', 'remaining_ms'].sort(),
    )
    expect(created.deadline - created.accepted_at).toBe(created.minutes * 60_000)
    expect(created.status).toBe('HOLDING')
  }

  // 旧版列表（不带参数）：保持创建顺序，即使乙的截止时刻更早。
  const listRes = await request.get('/api/timers')
  expect(listRes.status()).toBe(200)
  const list = (await listRes.json()) as Array<{ id: number }>
  const ids = list.map((t) => t.id)
  expect(ids.indexOf(a.id)).toBeGreaterThanOrEqual(0)
  expect(ids.indexOf(a.id)).toBeLessThan(ids.indexOf(b.id))

  // 看板视图：同一服务端 now，且乙（截止更早）排在甲之前——与旧版列表顺序无关。
  const boardRes = await request.get('/api/timers?view=board')
  expect(boardRes.status()).toBe(200)
  const board = (await boardRes.json()) as Array<{ id: number; now: number }>
  const boardIds = board.map((t) => t.id)
  expect(boardIds.indexOf(b.id)).toBeLessThan(boardIds.indexOf(a.id))
  expect(new Set(board.map((t) => t.now)).size).toBe(1)

  // 单条读取：同一不可变时刻，结构不变。
  const oneRes = await request.get(`/api/timers/${a.id}`)
  expect(oneRes.status()).toBe(200)
  const one = await oneRes.json()
  expect(one.accepted_at).toBe(a.accepted_at)
  expect(one.deadline).toBe(a.deadline)
  expect(one.label).toBe(a.label)
})
