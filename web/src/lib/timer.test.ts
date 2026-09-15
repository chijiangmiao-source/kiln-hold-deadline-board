import { describe, expect, it } from 'vitest'
import {
  SKEW_THRESHOLD_MS,
  formatRemaining,
  formatUtc,
  isClockSkewed,
  remainingAt,
  serverOffset,
  statusAt,
} from './timer'

describe('statusAt 临界规则', () => {
  const deadline = 1_000_000

  it('now < deadline 为 HOLDING', () => {
    expect(statusAt(deadline - 1, deadline)).toBe('HOLDING')
    expect(statusAt(0, deadline)).toBe('HOLDING')
  })

  it('临界毫秒归 READY', () => {
    expect(statusAt(deadline, deadline)).toBe('READY')
  })

  it('now > deadline 为 READY', () => {
    expect(statusAt(deadline + 1, deadline)).toBe('READY')
  })
})

describe('remainingAt', () => {
  it('取 max(0, deadline-now)', () => {
    expect(remainingAt(999_000, 1_000_000)).toBe(1000)
    expect(remainingAt(1_000_000, 1_000_000)).toBe(0)
    expect(remainingAt(1_500_000, 1_000_000)).toBe(0)
  })
})

describe('isClockSkewed', () => {
  it('偏差超过 30000 毫秒才提示，恰好 30000 不提示', () => {
    expect(isClockSkewed(SKEW_THRESHOLD_MS)).toBe(false)
    expect(isClockSkewed(-SKEW_THRESHOLD_MS)).toBe(false)
    expect(isClockSkewed(SKEW_THRESHOLD_MS + 1)).toBe(true)
    expect(isClockSkewed(-SKEW_THRESHOLD_MS - 1)).toBe(true)
  })
})

describe('serverOffset', () => {
  it('以请求中点估算服务端与本地时钟差', () => {
    // 服务端 1000，本地发送 100、收到 300 → 中点 200 → 偏差 800
    expect(serverOffset(1000, 100, 300)).toBe(800)
    expect(serverOffset(1000, 900, 1100)).toBe(0)
    expect(serverOffset(1000, 2000, 2400)).toBe(-1200)
  })
})

describe('formatRemaining', () => {
  it('格式化为 hh:mm:ss.mmm', () => {
    expect(formatRemaining(0)).toBe('00:00:00.000')
    expect(formatRemaining(999)).toBe('00:00:00.999')
    expect(formatRemaining(59_999)).toBe('00:00:59.999')
    expect(formatRemaining(60_000)).toBe('00:01:00.000')
    expect(formatRemaining(3_599_999)).toBe('00:59:59.999')
    expect(formatRemaining(3_600_000)).toBe('01:00:00.000')
    expect(formatRemaining(180 * 60_000)).toBe('03:00:00.000')
  })

  it('负值按 0 处理', () => {
    expect(formatRemaining(-5)).toBe('00:00:00.000')
  })
})

describe('formatUtc', () => {
  it('输出 UTC ISO 毫秒', () => {
    expect(formatUtc(0)).toBe('1970-01-01T00:00:00.000Z')
    expect(formatUtc(1_757_923_200_123)).toBe(new Date(1_757_923_200_123).toISOString())
  })
})
