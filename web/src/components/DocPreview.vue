<template>
  <div class="doc" v-if="api">
    <div class="doc-head">
      <span class="method big" :class="api.method">{{ api.method }}</span>
      <span class="path mono">{{ api.path }}</span>
      <el-tag size="small" :type="statusType">{{ statusLabel }}</el-tag>
      <div class="grow"></div>
      <el-tooltip content="复制 Mock 完整地址"><el-button size="small" text @click="copyMock"><el-icon><CopyDocument /></el-icon>Mock地址</el-button></el-tooltip>
      <el-tooltip content="复制 curl 命令"><el-button size="small" text @click="copyCurl"><el-icon><CopyDocument /></el-icon>curl</el-button></el-tooltip>
      <el-button size="small" type="primary" plain @click="$emit('debug')">调试</el-button>
      <el-button size="small" @click="$emit('edit')">编辑</el-button>
      <el-button size="small" type="danger" plain @click="del"><el-icon><Delete /></el-icon></el-button>
    </div>
    <div v-if="api.owner || api.remark" class="meta">
      <span v-if="api.owner">负责人:{{ api.owner }}</span>
      <span v-if="api.remark" class="remark">{{ api.remark }}</span>
    </div>

    <section v-if="api.pathParams?.length">
      <h4>Path 参数</h4>
      <table><thead><tr><th>名称</th><th>类型</th><th>必填</th><th>说明</th><th>示例</th></tr></thead>
      <tbody><tr v-for="p in api.pathParams" :key="p.name">
        <td class="mono">{{ p.name }}</td><td>{{ p.type }}</td><td><el-tag size="small" type="danger">必填</el-tag></td>
        <td>{{ p.desc }}</td><td class="mono">{{ p.example }}</td></tr></tbody></table>
    </section>

    <section v-if="api.queryParams?.length">
      <h4>Query 参数</h4>
      <table><thead><tr><th>名称</th><th>类型</th><th>必填</th><th>说明</th><th>示例</th></tr></thead>
      <tbody><tr v-for="p in api.queryParams" :key="p.name">
        <td class="mono">{{ p.name }}</td><td>{{ p.type }}</td>
        <td><el-tag size="small" :type="p.required ? 'danger' : 'info'">{{ p.required ? '必填' : '可选' }}</el-tag></td>
        <td>{{ p.desc }}</td><td class="mono">{{ p.example }}</td></tr></tbody></table>
    </section>

    <section v-if="api.headerParams?.length">
      <h4>Header</h4>
      <table><thead><tr><th>Key</th><th>示例值</th><th>备注</th></tr></thead>
      <tbody><tr v-for="h in api.headerParams" :key="h.key">
        <td class="mono">{{ h.key }}</td><td class="mono">{{ h.value }}</td><td>{{ h.remark }}</td></tr></tbody></table>
    </section>

    <section v-if="api.bodySchema || api.bodyExample">
      <h4>Body(application/json)</h4>
      <div v-if="api.bodySchema" class="sec-block">
        <b>Schema</b>
        <pre class="code-block">{{ pretty(api.bodySchema) }}</pre>
      </div>
      <div v-if="api.bodyExample" class="sec-block">
        <b>示例</b>
        <pre class="code-block">{{ pretty(api.bodyExample) }}</pre>
      </div>
    </section>

    <section v-if="api.responses?.length">
      <h4>响应定义</h4>
      <div v-for="r in api.responses" :key="r.code" class="resp">
        <div class="resp-line">
          <el-tag :type="String(r.code).startsWith('2') ? 'success' : (String(r.code).startsWith('4') ? 'warning' : 'danger')" size="small">{{ r.code }}</el-tag>
          <span>{{ r.desc }}</span>
        </div>
        <pre v-if="r.example" class="code-block">{{ pretty(r.example) }}</pre>
      </div>
    </section>

    <section>
      <h4>Mock</h4>
      <div class="mock-line">
        <span class="mono mock-url">{{ mockAddr }}</span>
        <el-button size="small" text type="primary" @click="copyMock">复制</el-button>
      </div>
      <div class="mock-cfg">
        <el-tag v-if="api.mock?.delayMs" size="small" type="warning">延时 {{ api.mock.delayMs }}ms</el-tag>
        <el-tag v-if="api.mock?.forceCode" size="small" type="danger">强制返回 {{ api.mock.forceCode }}</el-tag>
        <el-tag v-if="api.mock?.customJson" size="small" type="info">自定义返回</el-tag>
        <el-tag v-if="api.status === 'deprecated'" size="small">废弃模式:{{ depLabel }}</el-tag>
        <span v-if="!api.mock?.delayMs && !api.mock?.forceCode && !api.mock?.customJson && api.status !== 'deprecated'" class="none">默认:返回成功示例</span>
      </div>
    </section>
  </div>
  <el-empty v-else description="从左侧选择一个接口查看文档" />
</template>

<script setup>
import { computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api as apiClient, useAuth } from '../store'
import { copyText, buildCurl, mockUrl, prettyJSON } from '../utils'

const props = defineProps({
  api: { type: Object, default: null },
  projectId: { type: Number, required: true },
  project: { type: Object, default: null }
})
const emit = defineEmits(['edit', 'debug', 'deleted'])
const auth = useAuth()

const statusMap = { developing: ['开发中', 'primary'], done: ['已完成', 'success'], deprecated: ['已废弃', 'warning'] }
const statusLabel = computed(() => statusMap[props.api?.status]?.[0] || '')
const statusType = computed(() => statusMap[props.api?.status]?.[1] || 'info')
const depLabel = computed(() => ({ normal: '照常', '404': '返回404', tip: '返回提示' }[props.api?.mock?.deprecatedMode] || '照常'))
const mockAddr = computed(() => props.api ? mockUrl(auth.meta, props.projectId, props.api.path) : '')

function pretty(s) { return prettyJSON(s) }

async function copyMock() {
  await copyText(mockAddr.value)
  ElMessage.success('Mock 地址已复制')
}
async function copyCurl() {
  await copyText(buildCurl(props.api, mockUrl(auth.meta, props.projectId, '')))
  ElMessage.success('curl 已复制')
}

async function del() {
  try {
    await ElMessageBox.confirm(`删除接口「${props.api.name}」?`, '删除', { type: 'warning' })
  } catch { return }
  try {
    await apiClient.delete(`/apis/${props.api.id}`, { projectId: props.projectId })
    ElMessage.success('已删除')
    emit('deleted')
  } catch (e) { ElMessage.error(e.message) }
}
</script>

<style scoped>
.doc { padding: 16px 22px; }
.doc-head { display: flex; align-items: center; gap: 10px; }
.method.big { font-weight: 800; font-size: 16px; }
.path { font-size: 15px; background: #eff2f5; padding: 3px 10px; border-radius: 4px; }
.grow { flex: 1; }
.meta { color: #909399; font-size: 13px; margin: 8px 0 2px; display: flex; gap: 16px; }
h4 { margin: 22px 0 8px; font-size: 14px; color: #303133; border-left: 3px solid #409eff; padding-left: 8px; }
table { width: 100%; border-collapse: collapse; font-size: 13px; }
th, td { border: 1px solid #e4e7ed; padding: 6px 10px; text-align: left; }
th { background: #f5f7fa; color: #606266; font-weight: 500; }
.sec-block { margin-bottom: 8px; }
.sec-block b { font-size: 13px; color: #606266; }
.resp { margin-bottom: 10px; }
.resp-line { display: flex; gap: 8px; align-items: center; margin-bottom: 4px; font-size: 13px; }
.mock-line { display: flex; align-items: center; gap: 10px; }
.mock-url { background: #f0f9eb; border: 1px solid #e1f3d8; padding: 4px 10px; border-radius: 4px; font-size: 13px; }
.mock-cfg { display: flex; gap: 8px; margin-top: 8px; align-items: center; }
.none { color: #909399; font-size: 12px; }
</style>
