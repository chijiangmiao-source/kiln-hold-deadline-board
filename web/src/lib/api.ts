import type { TimerState } from './timer'

const BASE = '/api'

/** ApiError 携带 HTTP 状态码与服务端错误消息。 */
export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(`${BASE}${path}`, init)
  } catch {
    throw new ApiError(0, '无法连接 API')
  }
  const body: unknown = await res.json().catch(() => ({}))
  if (!res.ok) {
    const msg =
      typeof body === 'object' &&
      body !== null &&
      'error' in body &&
      typeof (body as { error: unknown }).error === 'string'
        ? (body as { error: string }).error
        : `请求失败（HTTP ${res.status}）`
    throw new ApiError(res.status, msg)
  }
  return body as T
}

/** createTimer 提交窑位标签与分钟数，返回服务端计时状态。 */
export function createTimer(label: string, minutes: number): Promise<TimerState> {
  return request<TimerState>('/timers', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ label, minutes }),
  })
}

/** getTimer 读取同一不可变计时（刷新或 API 重启后仍是同一截止时刻）。 */
export function getTimer(id: number): Promise<TimerState> {
  return request<TimerState>(`/timers/${id}`)
}
