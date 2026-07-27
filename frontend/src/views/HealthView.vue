<script setup lang="ts">
import { computed } from 'vue'
import { ArrowLeft, LocateFixed, ShieldCheck } from '@lucide/vue'
import type { Finding, GateLensContext, ViewID } from '../types'

const props = defineProps<{ context: GateLensContext; findings: Finding[] }>()
const emit = defineEmits<{ navigate: [view: ViewID]; locate: [targetID: string] }>()
const errors = computed(() => props.findings.filter((finding) => finding.severity === 'error').length)
const warnings = computed(() => props.findings.filter((finding) => finding.severity === 'warning').length)
</script>

<template>
  <section class="view">
    <div class="page-header">
      <div><p class="eyebrow">静态解析检查</p><h1>配置健康</h1><p>快照 {{ context.snapshot.observedAt }} 发现 {{ findings.length }} 个问题。</p></div>
      <button class="secondary-button" type="button" @click="emit('navigate', 'topology')"><ArrowLeft :size="15" />返回拓扑</button>
    </div>
    <div class="finding-summary">
      <div><strong class="text-error">{{ errors }}</strong><span>错误</span></div>
      <div><strong class="text-warning">{{ warnings }}</strong><span>警告</span></div>
      <div><strong class="text-healthy">{{ findings.length ? 0 : 1 }}</strong><span>{{ findings.length ? '待处理' : '快照健康' }}</span></div>
    </div>
    <section class="table-panel">
      <table>
        <thead><tr><th>严重度</th><th>问题</th><th>影响资源</th><th>检测依据</th><th><span class="sr-only">操作</span></th></tr></thead>
        <tbody>
          <tr v-for="finding in findings" :key="finding.id">
            <td><span class="severity" :class="finding.severity">{{ finding.severity === 'error' ? '错误' : '警告' }}</span></td>
            <td><strong>{{ finding.title }}</strong></td><td>{{ finding.resource }}</td><td><code>{{ finding.basis }}</code></td>
            <td><button class="icon-button table-icon" type="button" title="在拓扑中定位" @click="emit('locate', finding.targetID)"><LocateFixed :size="16" /></button></td>
          </tr>
          <tr v-if="!findings.length"><td colspan="5"><div class="table-empty"><ShieldCheck :size="20" />当前快照没有配置问题。</div></td></tr>
        </tbody>
      </table>
    </section>
  </section>
</template>
