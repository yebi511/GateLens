<script setup lang="ts">
import { CheckCircle2, Menu, RefreshCw } from '@lucide/vue'
import type { GateLensContext } from '../types'

const props = defineProps<{
  context: GateLensContext | null
  namespace: string
  loading: boolean
}>()
const emit = defineEmits<{ menu: []; refresh: []; 'update:namespace': [value: string] }>()
</script>

<template>
  <header class="context-bar">
    <button class="icon-button mobile-menu" type="button" title="打开导航" @click="emit('menu')">
      <Menu :size="18" aria-hidden="true" />
    </button>
    <label>
      <span>入口集群</span>
      <select :disabled="!props.context">
        <option>{{ props.context?.cluster.name ?? '加载中' }}</option>
      </select>
    </label>
    <label>
      <span>命名空间</span>
      <select :value="props.namespace" @change="emit('update:namespace', ($event.target as HTMLSelectElement).value)">
        <option value="">全部命名空间</option>
        <option v-for="item in props.context?.namespaces.filter((item) => item !== 'all') ?? []" :key="item" :value="item">
          {{ item }}
        </option>
      </select>
    </label>
    <label>
      <span>快照</span>
      <select :disabled="!props.context">
        <option>{{ props.context?.snapshot.observedAt ?? '等待快照' }}</option>
      </select>
    </label>
    <div class="context-spacer" />
    <span class="sync-state" :class="{ syncing: props.loading }">
      <RefreshCw v-if="props.loading" :size="14" class="spin" aria-hidden="true" />
      <CheckCircle2 v-else :size="14" aria-hidden="true" />
      {{ props.loading ? '正在同步' : '同步完成' }}
    </span>
    <button class="icon-button" type="button" title="刷新数据" :disabled="props.loading" @click="emit('refresh')">
      <RefreshCw :size="17" :class="{ spin: props.loading }" aria-hidden="true" />
    </button>
  </header>
</template>
