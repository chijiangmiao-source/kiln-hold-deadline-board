<script setup lang="ts">
import { formatRemaining, formatUtc, remainingAt, statusAt, type TimerState } from '../lib/timer'

// 看板卡片沿用与详情页相同的状态、剩余毫秒与时钟校准逻辑：
// 一切判定基于父组件按服务端时刻校准的 nowEstimate，绝不使用浏览器时间重排。
defineProps<{
  timers: TimerState[] | null
  nowEstimate: number
  loadError: boolean
}>()

const emit = defineEmits<{ select: [timer: TimerState]; back: [] }>()
</script>

<template>
  <section class="card" data-testid="timer-board">
    <header class="status-header">
      <h2>窑位看板</h2>
      <button type="button" class="secondary" data-testid="board-back" @click="emit('back')">
        返回
      </button>
    </header>

    <p v-if="loadError" class="warning" role="alert" data-testid="board-error">
      暂时无法加载看板，将按既有轮询自动重试。
    </p>
    <p v-else-if="timers === null" class="board-hint" data-testid="board-loading">正在加载…</p>
    <p v-else-if="timers.length === 0" class="board-hint" data-testid="board-empty">
      暂无计时记录
    </p>

    <ul v-if="timers !== null && timers.length > 0" class="board-list" data-testid="board-list">
      <li v-for="t in timers" :key="t.id">
        <button
          type="button"
          class="board-item"
          data-testid="board-item"
          :data-id="t.id"
          :data-deadline="t.deadline"
          @click="emit('select', t)"
        >
          <span class="board-item-head">
            <span class="board-label" data-testid="board-item-label">{{ t.label }}</span>
            <span
              class="badge"
              :class="statusAt(nowEstimate, t.deadline).toLowerCase()"
              data-testid="board-item-status"
            >
              {{ statusAt(nowEstimate, t.deadline) }}
            </span>
          </span>
          <span class="board-remaining" data-testid="board-item-remaining">
            {{ formatRemaining(remainingAt(nowEstimate, t.deadline)) }}
          </span>
          <span class="board-deadline">截止 {{ formatUtc(t.deadline) }}</span>
        </button>
      </li>
    </ul>
  </section>
</template>
