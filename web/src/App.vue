<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import TimerForm from './components/TimerForm.vue'
import TimerStatus from './components/TimerStatus.vue'
import { ApiError, createTimer, getTimer } from './lib/api'
import { isClockSkewed, serverOffset, type TimerState } from './lib/timer'

const STORAGE_KEY = 'kiln.timerId'
const TICK_MS = 50
const POLL_MS = 5000

const timer = ref<TimerState | null>(null)
const offset = ref(0)
const nowEstimate = ref(Date.now())
const loading = ref(false)
const submitError = ref('')
const apiDown = ref(false)

let tickHandle: ReturnType<typeof setInterval> | undefined
let pollHandle: ReturnType<typeof setInterval> | undefined

// applyState 用 API 返回的当前时刻校准本地倒计时。
function applyState(state: TimerState, sentAt: number, receivedAt: number) {
  timer.value = state
  offset.value = serverOffset(state.now, sentAt, receivedAt)
  nowEstimate.value = Date.now() + offset.value
  apiDown.value = false
}

async function refresh(id: number) {
  const sentAt = Date.now()
  try {
    const state = await getTimer(id)
    applyState(state, sentAt, Date.now())
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) {
      reset()
      return
    }
    apiDown.value = true
  }
}

async function onSubmit(label: string, minutes: number) {
  loading.value = true
  submitError.value = ''
  const sentAt = Date.now()
  try {
    const state = await createTimer(label, minutes)
    localStorage.setItem(STORAGE_KEY, String(state.id))
    applyState(state, sentAt, Date.now())
  } catch (err) {
    submitError.value = err instanceof Error ? err.message : '创建失败'
  } finally {
    loading.value = false
  }
}

function reset() {
  localStorage.removeItem(STORAGE_KEY)
  timer.value = null
  submitError.value = ''
}

// 偏差超过 30000 毫秒仅提示，不改变状态判定。
const skewed = computed(() => timer.value !== null && isClockSkewed(offset.value))

onMounted(async () => {
  tickHandle = setInterval(() => {
    nowEstimate.value = Date.now() + offset.value
  }, TICK_MS)
  // 刷新后按 localStorage 中的 id 读回同一不可变计时。
  const saved = localStorage.getItem(STORAGE_KEY)
  if (saved) {
    const id = Number(saved)
    if (Number.isInteger(id) && id > 0) {
      await refresh(id)
    } else {
      localStorage.removeItem(STORAGE_KEY)
    }
  }
  // 周期性地以服务端时刻重新校准（API 重启恢复后自动续作）。
  pollHandle = setInterval(() => {
    if (timer.value) void refresh(timer.value.id)
  }, POLL_MS)
})

onBeforeUnmount(() => {
  clearInterval(tickHandle)
  clearInterval(pollHandle)
})
</script>

<template>
  <main>
    <h1>陶瓷窑保温计时</h1>
    <TimerStatus
      v-if="timer"
      :timer="timer"
      :now-estimate="nowEstimate"
      :offset="offset"
      :skewed="skewed"
      :api-down="apiDown"
      @reset="reset"
    />
    <TimerForm v-else :loading="loading" :server-error="submitError" @submit="onSubmit" />
  </main>
</template>
