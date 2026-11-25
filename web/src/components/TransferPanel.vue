<template>
  <div class="panel">
    <div class="sec">
      <h4>导出</h4>
      <div class="row">
        <el-button size="small" @click="exportFile(`projects/${projectId}/export/openapi?format=yaml`, 'yaml')">
          OpenAPI 3.0 (yaml)
        </el-button>
        <el-button size="small" @click="exportFile(`projects/${projectId}/export/openapi?format=json`, 'json')">
          OpenAPI 3.0 (json)
        </el-button>
        <el-button size="small" @click="exportFile(`projects/${projectId}/export/json`, 'json')">项目备份 (JSON)</el-button>
        <el-button size="small" @click="exportFile(`projects/${projectId}/export/html`, 'html')">离线 HTML 文档</el-button>
      </div>
      <div class="tip">导出 OpenAPI 时,Mock 专属配置(延时/强制错误码/自定义返回/废弃模式)无法映射,将被丢弃;公共路径前缀写回 servers</div>
    </div>

    <div class="sec">
      <h4>导入 OpenAPI 3.0(yaml / json)</h4>
      <input type="file" accept=".yaml,.yml,.json" @change="pickFile" />
      <el-input v-model="content" type="textarea" :rows="6" class="mono" style="margin-top:8px"
        placeholder="或直接粘贴 OpenAPI 内容;servers 中的 basePath 会自动拼接到接口路径" />
      <el-button type="primary" size="small" style="margin-top:8px" :loading="analyzing" @click="analyze">
        解析预检
      </el-button>

      <template v-if="plan">
        <el-alert type="info" :closable="false" style="margin-top:12px"
          :title="`共 ${plan.total} 个接口:新建 ${plan.toCreate} 个,覆盖 ${plan.toOverwrite} 个` + (plan.basePath ? `,basePath=${plan.basePath}` : '')" />
        <el-table :data="plan.items" size="small" style="margin-top:10px" max-height="360">
          <el-table-column prop="method" label="方式" width="80">
            <template #default="{ row }"><span class="method mono" :class="row.method">{{ row.method }}</span></template>
          </el-table-column>
          <el-table-column prop="path" label="路径" class-name="mono" min-width="180" />
          <el-table-column prop="name" label="名称" min-width="140" />
          <el-table-column label="冲突" width="90">
            <template #default="{ row }">
              <el-tag v-if="row.exists" size="small" type="warning">已存在</el-tag>
              <el-tag v-else-if="row.conflictPath" size="small" type="danger">模板歧义</el-tag>
              <el-tag v-else size="small" type="success">新建</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="导入动作" width="150">
            <template #default="{ row }">
              <el-select v-model="row.action" size="small" :disabled="row.conflictPath">
                <el-option label="跳过" value="skip" />
                <el-option label="覆盖" value="overwrite" />
              </el-select>
            </template>
          </el-table-column>
        </el-table>
        <div class="row" style="margin-top:10px">
          <el-button size="small" @click="setAll('overwrite')">全部覆盖</el-button>
          <el-button size="small" @click="setAll('skip')">全部跳过</el-button>
          <div class="grow"></div>
          <el-button size="small" type="primary" :loading="applying" @click="apply">按上述动作导入</el-button>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, useAuth } from '../store'

const props = defineProps({
  projectId: { type: Number, required: true },
  projectName: { type: String, default: 'project' }
})
const emit = defineEmits(['changed'])
const auth = useAuth()

const content = ref('')
const analyzing = ref(false)
const applying = ref(false)
const plan = ref(null)

function pickFile(ev) {
  const f = ev.target.files?.[0]
  if (!f) return
  const reader = new FileReader()
  reader.onload = () => { content.value = String(reader.result || '') }
  reader.readAsText(f)
}

async function analyze() {
  if (!content.value.trim()) { ElMessage.warning('请选择文件或粘贴内容'); return }
  analyzing.value = true
  plan.value = null
  try {
    const d = await api.post(`/projects/${props.projectId}/import/openapi/analyze`, { content: content.value },
      { projectId: props.projectId })
    d.items.forEach(it => { it.action = it.conflictPath ? 'skip' : (it.exists ? 'overwrite' : 'overwrite') })
    plan.value = d
  } catch (e) { ElMessage.error(e.message) } finally { analyzing.value = false }
}

function setAll(action) { plan.value?.items.forEach(it => { if (!it.conflictPath) it.action = action }) }

async function apply() {
  if (!plan.value) return
  applying.value = true
  try {
    const decisions = plan.value.items.map(it => ({ method: it.method, path: it.path, action: it.action }))
    const d = await api.post(`/projects/${props.projectId}/import/openapi/apply`,
      { content: content.value, decisions }, { projectId: props.projectId })
    let msg = `新建 ${d.created},更新 ${d.updated},跳过 ${d.skipped}`
    if (d.conflicts?.length) msg += `,模板冲突 ${d.conflicts.length}:${d.conflicts.join('、')}`
    ElMessage.success('导入完成:' + msg)
    plan.value = null
    content.value = ''
    emit('changed')
  } catch (e) { ElMessage.error(e.message) } finally { applying.value = false }
}

async function exportFile(path, ext) {
  if (path.includes('openapi')) {
    try {
      await ElMessageBox.confirm(
        'Mock 专属配置(延时、强制错误码、自定义返回、废弃模式)无法映射为 OpenAPI,导出时将被丢弃。继续?',
        '导出 OpenAPI', { type: 'info', confirmButtonText: '继续导出' })
    } catch { return }
  }
  try {
    const resp = await fetch('/api/' + path, { headers: { 'X-Auth-Token': auth.projectToken(props.projectId) || auth.adminToken } })
    if (!resp.ok) throw new Error('导出失败(HTTP ' + resp.status + ')')
    const blob = await resp.blob()
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    const base = props.projectName.replace(/[^\w\u4e00-\u9fa5-]+/g, '-') || 'project'
    a.download = `${base}.${ext}`
    a.click()
    URL.revokeObjectURL(a.href)
  } catch (e) { ElMessage.error(e.message) }
}
</script>

<style scoped>
.panel { padding: 14px; }
.sec { margin-bottom: 22px; }
.sec h4 { margin: 0 0 10px; border-left: 3px solid #409eff; padding-left: 8px; }
.row { display: flex; gap: 10px; align-items: center; }
.grow { flex: 1; }
.tip { color: #909399; font-size: 12px; margin-top: 6px; }
</style>
