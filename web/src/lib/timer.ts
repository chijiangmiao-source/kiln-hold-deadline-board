/** TimerState 与 Go API 的计时视图一一对应（时刻均为 UTC 毫秒）。 */
export interface TimerState {
  id: number
  label: string
  minutes: number
  accepted_at: number
  deadline: number
  now: number
  status: TimerStatus
  remaining_ms: number
}

export type TimerStatus = 'HOLDING' | 'READY'

/** SKEW_THRESHOLD_MS 浏览器与服务端时钟偏差的提示阈值（超过才提示）。 */
export const SKEW_THRESHOLD_MS = 30_000

/** statusAt 实现唯一状态规则：now < deadline 为 HOLDING，否则 READY（临界毫秒归 READY）。 */
export function statusAt(nowMs: number, deadlineMs: number): TimerStatus {
  return nowMs < deadlineMs ? 'HOLDING' : 'READY'
}

/** remainingAt 取 max(0, deadline-now)。 */
export function remainingAt(nowMs: number, deadlineMs: number): number {
  return Math.max(0, deadlineMs - nowMs)
}

/**
 * serverOffset 估算「服务端时刻 − 本地时刻」的毫秒差，
 * 以请求发出与响应收到的本地中点近似服务端采样时刻。
 */
export function serverOffset(serverNow: number, sentAt: number, receivedAt: number): number {
  return serverNow - (sentAt + receivedAt) / 2
}

/** isClockSkewed 偏差绝对值超过阈值（30000 毫秒）才视为偏差过大。 */
export function isClockSkewed(offsetMs: number, thresholdMs: number = SKEW_THRESHOLD_MS): boolean {
  return Math.abs(offsetMs) > thresholdMs
}

/** formatRemaining 把剩余毫秒格式化为 hh:mm:ss.mmm，负值按 0 处理。 */
export function formatRemaining(ms: number): string {
  const clamped = Math.max(0, Math.floor(ms))
  const hours = Math.floor(clamped / 3_600_000)
  const minutes = Math.floor((clamped % 3_600_000) / 60_000)
  const seconds = Math.floor((clamped % 60_000) / 1000)
  const millis = clamped % 1000
  const pad = (n: number, width = 2) => String(n).padStart(width, '0')
  return `${pad(hours)}:${pad(minutes)}:${pad(seconds)}.${pad(millis, 3)}`
}

/** formatUtc 把 UTC 毫秒格式化为 ISO 字符串。 */
export function formatUtc(ms: number): string {
  return new Date(ms).toISOString()
}
