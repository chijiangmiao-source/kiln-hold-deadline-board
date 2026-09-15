<script setup lang="ts">
import { ref } from 'vue'

const props = defineProps<{ loading: boolean; serverError: string }>()
const emit = defineEmits<{ submit: [label: string, minutes: number] }>()

const label = ref('')
const minutes = ref<number | string | null>(null)
const localError = ref('')

function onSubmit() {
  const trimmed = label.value.trim()
  if (!trimmed) {
    localError.value = '窑位标签不能为空'
    return
  }
  const m = minutes.value
  if (typeof m !== 'number' || !Number.isInteger(m) || m < 1 || m > 180) {
    localError.value = '分钟数必须是 1 至 180 的十进制整数'
    return
  }
  localError.value = ''
  emit('submit', trimmed, m)
}
</script>

<template>
  <form class="card" data-testid="timer-form" novalidate @submit.prevent="onSubmit">
    <h2>进入保温段</h2>
    <div class="field">
      <label for="kiln-label">窑位标签</label>
      <input
        id="kiln-label"
        v-model="label"
        type="text"
        maxlength="80"
        placeholder="例如：窑位A-3"
        autocomplete="off"
      />
    </div>
    <div class="field">
      <label for="kiln-minutes">保温分钟数（1–180）</label>
      <input
        id="kiln-minutes"
        v-model.number="minutes"
        type="number"
        min="1"
        max="180"
        step="1"
        placeholder="例如：45"
      />
    </div>
    <p v-if="localError || props.serverError" class="error" role="alert" data-testid="form-error">
      {{ localError || props.serverError }}
    </p>
    <button type="submit" :disabled="props.loading">
      {{ props.loading ? '写入中…' : '开始保温' }}
    </button>
  </form>
</template>
