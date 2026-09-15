<script setup lang="ts">
import { computed } from 'vue'
import { formatRemaining, formatUtc, remainingAt, statusAt, type TimerState } from '../lib/timer'

const props = defineProps<{
  timer: TimerState
  nowEstimate: number
  offset: number
  skewed: boolean
  apiDown: boolean
}>()

const emit = defineEmits<{ reset: [] }>()

// 状态与剩余毫秒只按（校准后的）服务端当前时刻推导，临界毫秒归 READY。
const status = computed(() => statusAt(props.nowEstimate, props.timer.deadline))
const remaining = computed(() => remainingAt(props.nowEstimate, props.timer.deadline))
</script>

<template>
  <section class="card" data-testid="timer-status">
    <header class="status-header">
      <h2>窑位 {{ timer.label }}</h2>
      <span
        class="badge"
        :class="status.toLowerCase()"
        data-testid="status"
        :data-deadline="timer.deadline"
        :data-accepted-at="timer.accepted_at"
      >
        {{ status }}
      </span>
    </header>

    <p class="countdown" data-testid="remaining">{{ formatRemaining(remaining) }}</p>
    <p class="remaining-ms">剩余 <span data-testid="remaining-ms">{{ remaining }}</span> 毫秒</p>

    <dl class="times">
      <div>
        <dt>开始时刻（UTC）</dt>
        <dd data-testid="accepted-at">{{ formatUtc(timer.accepted_at) }}</dd>
      </div>
      <div>
        <dt>截止时刻（UTC）</dt>
        <dd data-testid="deadline">{{ formatUtc(timer.deadline) }}</dd>
      </div>
    </dl>

    <p v-if="skewed" class="warning" role="alert" data-testid="skew-warning">
      浏览器时钟与服务端偏差超过 30 秒（约 {{ Math.round(offset) }}
      毫秒）；倒计时已按服务端时刻校准，状态判定不受影响。
    </p>
    <p v-if="apiDown" class="warning" data-testid="api-down">
      暂时无法连接 API，当前显示基于上次校准的服务端时刻。
    </p>

    <button type="button" class="secondary" @click="emit('reset')">新建计时</button>
  </section>
</template>
