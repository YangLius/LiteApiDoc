<template>
  <div class="panel">
    <div class="bar">
      <el-button size="small" @click="load"><el-icon><Refresh /></el-icon>&nbsp;刷新</el-button>
      <el-button size="small" type="danger" plain @click="clear">清空日志</el-button>
      <span class="tip">项目级保留最近 100 条(PRD 模块4.4)</span>
    </div>
    <el-table :data="logs" size="small" v-loading="loading" :default-sort="{ prop: 'id', order: 'descending' }">
      <el-table-column prop="method" label="方式" width="90">
        <template #default="{ row }"><span class="method mono" :class="row.method">{{ row.method }}</span></template>
      </el-table-column>
      <el-table-column prop="path" label="路径" class-name="mono" min-width="200" />
      <el-table-column prop="query" label="Query" class-name="mono" min-width="160" show-overflow-tooltip />
      <el-table-column prop="respCode" label="状态码" width="90">
        <template #default="{ row }">
          <el-tag size="small" :type="row.respCode < 300 ? 'success' : (row.respCode < 500 ? 'warning' : 'danger')">{{ row.respCode }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="durationMs" label="耗时" width="90">
        <template #default="{ row }">{{ row.durationMs }}ms</template>
      </el-table-column>
      <el-table-column prop="createdAt" label="时间" width="170">
        <template #default="{ row }">{{ fmtTime(row.createdAt) }}</template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '../store'
import { fmtTime } from '../utils'

const props = defineProps({ projectId: { type: Number, required: true } })
const logs = ref([])
const loading = ref(false)

async function load() {
  loading.value = true
  try { logs.value = await api.get(`/projects/${props.projectId}/mock-logs`) }
  catch (e) { ElMessage.error(e.message) }
  finally { loading.value = false }
}
onMounted(load)

async function clear() {
  try { await ElMessageBox.confirm('清空该项目全部 Mock 日志?', '清空', { type: 'warning' }) } catch { return }
  try {
    await api.delete(`/projects/${props.projectId}/mock-logs`, { projectId: props.projectId })
    load()
  } catch (e) { ElMessage.error(e.message) }
}
defineExpose({ load })
</script>

<style scoped>
.panel { padding: 14px; }
.bar { display: flex; align-items: center; gap: 10px; margin-bottom: 12px; }
.tip { color: #909399; font-size: 12px; }
</style>
