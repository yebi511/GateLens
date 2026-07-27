<script setup lang="ts">
import { computed, ref } from 'vue'
import { ExternalLink, Search } from '@lucide/vue'
import type { Resource } from '../types'

const props = defineProps<{ resources: Resource[] }>()
const emit = defineEmits<{ locate: [targetID: string] }>()
const search = ref('')
const visible = computed(() => {
  const query = search.value.trim().toLowerCase()
  return props.resources.filter((resource) => !query || `${resource.kind} ${resource.name} ${resource.namespace}`.toLowerCase().includes(query))
})
</script>

<template>
  <section class="view">
    <div class="page-header">
      <div><p class="eyebrow">配置事实</p><h1>资源</h1><p>当前快照中的 Gateway、Listener、HTTPRoute、Service 和 Endpoint。</p></div>
      <label class="search-field header-search"><Search :size="15" /><input v-model="search" placeholder="名称、Kind 或命名空间" /></label>
    </div>
    <section class="table-panel">
      <table>
        <thead><tr><th>Kind</th><th>名称</th><th>命名空间</th><th>状态</th><th>更新时间</th><th>关联问题</th><th><span class="sr-only">操作</span></th></tr></thead>
        <tbody>
          <tr v-for="resource in visible" :key="resource.id">
            <td><code>{{ resource.kind }}</code></td><td><strong>{{ resource.name }}</strong></td><td>{{ resource.namespace || '—' }}</td>
            <td><span class="resource-status" :class="`text-${resource.status}`"><i class="status-dot" :class="resource.status" />{{ resource.statusText }}</span></td>
            <td>{{ resource.updatedAt }}</td><td>{{ resource.findings || '—' }}</td>
            <td><button class="icon-button table-icon" type="button" title="在拓扑中查看" @click="emit('locate', resource.id)"><ExternalLink :size="16" /></button></td>
          </tr>
          <tr v-if="!visible.length"><td colspan="7"><div class="table-empty">没有匹配的资源。</div></td></tr>
        </tbody>
      </table>
    </section>
  </section>
</template>
