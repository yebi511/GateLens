<script setup lang="ts">
import { AlertTriangle, CheckCircle2, XCircle } from '@lucide/vue'
import type { Status } from '../types'

defineProps<{ status: Status; label: string }>()
</script>

<template>
  <span class="status-badge" :class="`status-${status}`">
    <CheckCircle2 v-if="status === 'healthy'" :size="13" aria-hidden="true" />
    <AlertTriangle v-else-if="status === 'warning'" :size="13" aria-hidden="true" />
    <XCircle v-else :size="13" aria-hidden="true" />
    {{ label }}
  </span>
</template>
