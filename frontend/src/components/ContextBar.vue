<script setup lang="ts">
import { computed } from 'vue'
import { CheckCircle2, Menu, RefreshCw } from '@lucide/vue'
import type { TopologyCluster } from '../types'

const props = defineProps<{
  clusters: TopologyCluster[]
  selectedCluster: string
  namespace: string
  loading: boolean
}>()
const emit = defineEmits<{ menu: []; refresh: []; 'update:selectedCluster': [value: string]; 'update:namespace': [value: string] }>()
const cluster = computed(() => props.clusters.find((item) => item.id === props.selectedCluster) ?? props.clusters[0] ?? null)
</script>

<template>
  <header class="context-bar">
    <button class="icon-button mobile-menu" type="button" title="打开导航" @click="emit('menu')">
      <Menu :size="18" aria-hidden="true" />
    </button>
    <label>
      <span>集群</span>
      <select :value="props.selectedCluster" :disabled="!props.clusters.length" @change="emit('update:selectedCluster', ($event.target as HTMLSelectElement).value)">
        <option v-if="!props.clusters.length" value="">加载中</option>
        <option v-for="item in props.clusters" :key="item.id" :value="item.id">{{ item.name }}</option>
      </select>
    </label>
    <label>
      <span>命名空间</span>
      <select :value="props.namespace" @change="emit('update:namespace', ($event.target as HTMLSelectElement).value)">
        <option value="">全部命名空间</option>
        <option v-for="item in cluster?.namespaces.filter((item) => item !== 'all') ?? []" :key="item" :value="item">
          {{ item }}
        </option>
      </select>
    </label>
    <label>
      <span>快照</span>
      <select :disabled="!cluster">
        <option>{{ cluster?.snapshot.observedAt ?? '等待快照' }}</option>
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
