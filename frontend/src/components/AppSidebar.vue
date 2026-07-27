<script setup lang="ts">
import { Activity, Boxes, Network, Route, ShieldAlert, Waypoints, X } from '@lucide/vue'
import type { GateLensContext, ViewID } from '../types'

const props = defineProps<{
  current: ViewID
  context: GateLensContext | null
  findingCount: number
  open: boolean
}>()
const emit = defineEmits<{ navigate: [view: ViewID]; close: [] }>()

const items = [
  { id: 'topology' as const, label: '拓扑', icon: Network },
  { id: 'envoy' as const, label: 'Envoy 配置', icon: Waypoints },
  { id: 'simulator' as const, label: '请求模拟', icon: Route },
  { id: 'health' as const, label: '配置健康', icon: ShieldAlert },
  { id: 'resources' as const, label: '资源', icon: Boxes },
]
</script>

<template>
  <aside class="sidebar" :class="{ open: props.open }">
    <div class="brand-row">
      <button class="brand" type="button" @click="emit('navigate', 'topology')">
        <img src="/gatelens-mark.svg" alt="" />
        <span>GateLens</span>
      </button>
      <button class="icon-button sidebar-close" type="button" title="关闭导航" @click="emit('close')">
        <X :size="18" aria-hidden="true" />
      </button>
    </div>
    <p class="workspace-label">AI 网关工作台</p>
    <nav class="nav-list" aria-label="主导航">
      <button
        v-for="item in items"
        :key="item.id"
        class="nav-item"
        :class="{ active: props.current === item.id }"
        type="button"
        @click="emit('navigate', item.id)"
      >
        <component :is="item.icon" :size="17" aria-hidden="true" />
        <span>{{ item.label }}</span>
        <b v-if="item.id === 'health' && props.findingCount">{{ props.findingCount }}</b>
      </button>
    </nav>
    <div class="sidebar-foot">
      <div class="cluster-card">
        <Activity :size="16" aria-hidden="true" />
        <div>
          <strong>{{ props.context?.cluster.name ?? '正在连接集群' }}</strong>
          <small>{{ props.context?.cluster.version ?? '读取 Kubernetes 快照' }}</small>
        </div>
      </div>
    </div>
  </aside>
</template>
