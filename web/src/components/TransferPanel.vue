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
      <h4>从服务地址导入(自动探测 Swagger / OpenAPI)</h4>
      <div class="tip" style="margin:0 0 8px">
        每行一个地址,支持批量导入一个网关下的多套文档;可填服务基础地址(自动探测
        openapi.json、swagger/doc.json、/docs 页面等)或规范直链。Swagger 2.0 自动转换,
        代码注解工具(swag / springdoc / FastAPI)产出的规范均可导入
      </div>
      <el-input v-model="specUrlInput" type="textarea" :rows="3" class="mono"
        placeholder="http://192.168.1.10:8080&#10;http://host:8155/erp/cm&#10;http://host:8155/erp/hh170/openapi.json" />
      <div class="row" style="margin-top:8px">
        <el-checkbox v-model="saveSource" size="small">保存为同步源(多个地址全部保存)</el-checkbox>
        <el-checkbox v-model="insecure" size="small">跳过 HTTPS 证书校验(自签内网服务)</el-checkbox>
        <div class="grow"></div>
        <el-button v-if="savedSources.length" size="small" :loading="probing" @click="syncSaved">
          一键同步({{ savedSources.length }} 个源)
        </el-button>
        <el-button type="primary" size="small" :loading="probing" @click="probeInput">探测并分析</el-button>
      </div>
      <div v-if="savedSources.length" class="row" style="margin-top:6px">
        <span class="tip">已保存同步源 {{ savedSources.length }} 个(一键同步使用,不回填上方输入框)</span>
        <el-tooltip placement="top">
          <template #content><div style="max-width:440px;white-space:pre-line">{{ savedSources.join('\n') }}</div></template>
          <el-button link type="primary" size="small">查看</el-button>
        </el-tooltip>
        <el-button link type="danger" size="small" @click="clearSavedSources">清除</el-button>
      </div>
      <div class="row" style="margin-top:8px">
        <span class="tip">导入到分组:</span>
        <el-select v-model="targetGroupId" size="small" style="width:280px">
          <el-option v-for="g in groupOptions" :key="g.id" :value="g.id" :label="g.label" />
        </el-select>
        <span class="tip">同路径已有接口会随导入移入所选分组</span>
      </div>
      <el-input v-model="headerLines" type="textarea" :rows="2" class="mono" style="margin-top:8px"
        placeholder="拉取规范需认证时填请求头,每行一条,格式 Key: value,如 Authorization: Bearer xxx;保存后一键同步自动携带" />

      <template v-if="results">
        <el-alert v-if="results.some(r => r.error)" type="warning" :closable="false" style="margin-top:10px"
          title="部分地址拉取失败(未勾选):修正地址后可重新探测" />
        <el-table :data="results" size="small" style="margin-top:10px" max-height="420">
          <el-table-column type="expand">
            <template #default="{ row }">
              <el-table v-if="row.items?.length" :data="row.items" size="small" style="margin:4px 10px 8px 40px">
                <el-table-column prop="method" label="方式" width="80">
                  <template #default="{ row: it }"><span class="method mono" :class="it.method">{{ it.method }}</span></template>
                </el-table-column>
                <el-table-column prop="path" label="路径" class-name="mono" min-width="180" />
                <el-table-column prop="name" label="名称" min-width="140" />
                <el-table-column label="冲突" width="90">
                  <template #default="{ row: it }">
                    <el-tag v-if="it.exists" size="small" type="warning">已存在</el-tag>
                    <el-tag v-else-if="it.conflictPath" size="small" type="danger">模板歧义</el-tag>
                    <el-tag v-else size="small" type="success">新建</el-tag>
                  </template>
                </el-table-column>
                <el-table-column label="导入动作" width="120">
                  <template #default="{ row: it }">
                    <el-select v-model="it.action" size="small" :disabled="!!it.conflictPath">
                      <el-option label="跳过" value="skip" />
                      <el-option label="覆盖" value="overwrite" />
                    </el-select>
                  </template>
                </el-table-column>
              </el-table>
            </template>
          </el-table-column>
          <el-table-column width="44">
            <template #default="{ row }"><el-checkbox v-model="row.checked" :disabled="!!row.error" /></template>
          </el-table-column>
          <el-table-column label="文档" min-width="160" show-overflow-tooltip>
            <template #default="{ row }">{{ row.title || row.url }}</template>
          </el-table-column>
          <el-table-column label="接口数" width="80" align="center">
            <template #default="{ row }">{{ row.total }}</template>
          </el-table-column>
          <el-table-column label="新建/覆盖" width="92" align="center">
            <template #default="{ row }">{{ row.toCreate }} / {{ row.toOverwrite }}</template>
          </el-table-column>
          <el-table-column label="状态" min-width="180" show-overflow-tooltip>
            <template #default="{ row }">
              <el-tag v-if="row.error" size="small" type="danger">{{ row.error }}</el-tag>
              <el-tag v-else size="small" type="success">{{ row.specKind === 'swagger2' ? 'Swagger 2.0(已转换)' : 'OpenAPI 3' }}</el-tag>
            </template>
          </el-table-column>
        </el-table>
        <div class="row" style="margin-top:10px">
          <span class="tip">已勾选 {{ checkedCount }} / {{ results.length }} 个来源,共 {{ checkedTotal }} 个接口(展开行可逐个调整导入动作)</span>
          <div class="grow"></div>
          <el-button size="small" :disabled="applying" @click="results.forEach(r => { if (!r.error) markActions(r) })">全部设为覆盖</el-button>
          <el-button size="small" type="primary" :loading="applying" @click="applyChecked">导入勾选来源</el-button>
        </div>
      </template>
    </div>

    <div class="sec">
      <h4>导入 OpenAPI 3.0 / Swagger 2.0(yaml / json)</h4>
      <input type="file" accept=".yaml,.yml,.json" @change="pickFile" />
      <el-input v-model="content" type="textarea" :rows="6" class="mono" style="margin-top:8px"
        placeholder="或直接粘贴 OpenAPI / Swagger 内容;$ref 自动内联,Servers 中的 basePath 会自动拼接到接口路径" />
      <el-button type="primary" size="small" style="margin-top:8px" :loading="analyzing" @click="analyze">
        解析预检
      </el-button>
      <div class="row" style="margin-top:8px">
        <span class="tip">导入到分组:</span>
        <el-select v-model="targetGroupId" size="small" style="width:280px">
          <el-option v-for="g in groupOptions" :key="g.id" :value="g.id" :label="g.label" />
        </el-select>
      </div>
      <template v-if="plan">
        <el-alert v-if="plan.specKind" type="success" :closable="false" style="margin-top:12px"
          :title="`识别为 ${plan.specKind === 'swagger2' ? 'Swagger 2.0(已自动转换)' : 'OpenAPI 3.0'}` +
            (plan.title ? ` · ${plan.title}` : '')" />
        <el-alert v-if="plan.warnings?.length" type="warning" :closable="false" style="margin-top:8px"
          :title="plan.warnings.join(';')" />
        <el-alert type="info" :closable="false" style="margin-top:8px"
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
import { computed, ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, useAuth } from '../store'

const props = defineProps({
  projectId: { type: Number, required: true },
  projectName: { type: String, default: 'project' },
  specUrl: { type: String, default: '' },
  specHeaders: { type: String, default: '' }
})
const emit = defineEmits(['changed'])
const auth = useAuth()

// 地址输入框默认留空;已保存的同步源单独展示在下方“已保存同步源”行,不回填输入框
const specUrlInput = ref('')
const saveSource = ref(true)
const insecure = ref(false)
const probing = ref(false)
const analyzing = ref(false)
const applying = ref(false)
const plan = ref(null)      // 粘贴/文件导入的单源预检结果
const results = ref(null)   // 服务地址批量探测的多源结果
const content = ref('')

function storedHeadersToLines(raw) {
  try {
    const obj = raw ? JSON.parse(raw) : {}
    return Object.entries(obj || {}).map(([k, v]) => `${k}: ${v}`).join('\n')
  } catch { return '' }
}
const headerLines = ref(storedHeadersToLines(props.specHeaders))

// 解析“Key: value”每行一条的请求头文本
function parseHeaderLines() {
  const out = {}
  headerLines.value.split('\n').forEach(line => {
    const i = line.indexOf(':')
    if (i > 0) {
      const k = line.slice(0, i).trim()
      const v = line.slice(i + 1).trim()
      if (k) out[k] = v
    }
  })
  return out
}
// 已保存的同步源列表(兼容单地址字符串与 JSON 数组两种存法)
const savedSources = computed(() => {
  const v = props.specUrl || ''
  if (v.startsWith('[')) {
    try { return JSON.parse(v).filter(Boolean) } catch { return [] }
  }
  return v ? [v] : []
})

// 清除已保存的同步源(一键同步将不可用,可重新探测并勾选保存)
async function clearSavedSources() {
  try {
    await ElMessageBox.confirm('清除后「一键同步」将不可用(仍可在地址框重新探测导入)。确认清除?', '清除同步源', { type: 'warning', confirmButtonText: '清除' })
  } catch { return }
  try {
    await api.patch(`/projects/${props.projectId}`, { specUrl: '' }, { projectId: props.projectId })
    ElMessage.success('已清除同步源')
    emit('changed')
  } catch (e) { ElMessage.error(e.message) }
}
const checkedCount = computed(() => (results.value || []).filter(r => r.checked && !r.error).length)
const checkedTotal = computed(() => (results.value || []).filter(r => r.checked && !r.error).reduce((a, r) => a + (r.total || 0), 0))

// ---- 导入目标分组:0=未分组,-1=按来源自动建分组,>0=指定分组 ----
const targetGroupId = ref(0)
const groups = ref([])
const groupOptions = computed(() => {
  const byId = Object.fromEntries(groups.value.map(g => [g.id, g]))
  return [
    { id: 0, label: '未分组' },
    { id: -1, label: '按来源自动建分组(每个文档一个)' },
    ...groups.value.map(g => ({
      id: g.id,
      label: g.parentId && byId[g.parentId] ? `${byId[g.parentId].name} / ${g.name}` : g.name
    }))
  ]
})
async function loadGroups() {
  try { groups.value = await api.get(`/projects/${props.projectId}/groups`, { projectId: props.projectId }) || [] } catch { /* 分组加载失败不阻塞导入 */ }
}
onMounted(loadGroups)

// 解析某来源的目标分组;-1 模式按文档标题建/复用根分组(“管家婆财贸/工贸 CM 开放 API”等)
async function resolveGroupId(row) {
  if (targetGroupId.value > 0) return targetGroupId.value
  if (targetGroupId.value === -1) {
    const name = ((row.title || '').trim().slice(0, 30)) || '导入接口'
    let g = groups.value.find(x => !x.parentId && x.name === name)
    if (!g) {
      g = await api.post(`/projects/${props.projectId}/groups`, { name, parentId: 0 }, { projectId: props.projectId })
      groups.value.push(g)
    }
    return g.id
  }
  return 0
}

function pickFile(ev) {
  const f = ev.target.files?.[0]
  if (!f) return
  const reader = new FileReader()
  reader.onload = () => { content.value = String(reader.result || '') }
  reader.readAsText(f)
}

function markActions(d) {
  d.items?.forEach(it => { it.action = it.conflictPath ? 'skip' : 'overwrite' })
}

// 批量探测分析:urls 每项独立发现+解析;返回结果行(apply 凭 analysisId 引用内容)
async function probe(urls, save) {
  const list = (urls || []).map(u => (u || '').trim()).filter(Boolean)
  if (!list.length) { ElMessage.warning('请填写服务地址或规范文档地址'); return null }
  probing.value = true
  results.value = null
  try {
    const headers = parseHeaderLines()
    // 保存同步源时,空请求头 + 已存有请求头 → 显式传 {} 清除;否则空=undefined 沿用后端已存
    const hasStored = !!(props.specHeaders && props.specHeaders !== '{}')
    const payloadHeaders = Object.keys(headers).length ? headers : (save && hasStored ? {} : undefined)
    const d = await api.post(`/projects/${props.projectId}/import/spec-url/analyze`,
      { urls: list, save: !!save, insecure: insecure.value, headers: payloadHeaders }, { projectId: props.projectId, timeout: 180000 })
    const rows = d.results?.length ? d.results : [d] // 兼容旧单地址响应
    rows.forEach(r => { r.checked = !r.error; markActions(r) })
    results.value = rows
    if (save) emit('changed')
    return rows
  } catch (e) { ElMessage.error(e.message); return null } finally { probing.value = false }
}

function probeInput() { probe(specUrlInput.value.split('\n'), saveSource.value) }

// 按分析ID批量导入勾选来源;汇总各源结果
async function applyChecked() {
  const rows = (results.value || []).filter(r => r.checked && !r.error && r.analysisId)
  if (!rows.length) { ElMessage.warning('请先勾选要导入的来源'); return }
  applying.value = true
  try {
    let created = 0, updated = 0, skipped = 0
    const allConflicts = []
    for (const r of rows) {
      const gid = await resolveGroupId(r)
      const decisions = r.items.map(it => ({ method: it.method, path: it.path, action: it.action || 'overwrite' }))
      const d = await api.post(`/projects/${props.projectId}/import/openapi/apply`,
        { analysisId: r.analysisId, groupId: gid || undefined, decisions }, { projectId: props.projectId, timeout: 600000 })
      created += d.created; updated += d.updated; skipped += d.skipped
      if (d.conflicts?.length) allConflicts.push(...d.conflicts)
    }
    let msg = `${rows.length} 个来源:新建 ${created},更新 ${updated},跳过 ${skipped}`
    if (allConflicts.length) msg += `,模板冲突 ${allConflicts.length}:${allConflicts.slice(0, 5).join('、')}${allConflicts.length > 5 ? '…' : ''}`
    ElMessage.success('导入完成:' + msg)
    results.value = null
    emit('changed')
  } catch (e) { ElMessage.error(e.message) } finally { applying.value = false }
}

// 一键同步:遍历已保存的全部同步源 → 拉取确认 → 全部覆盖导入
async function syncSaved() {
  const urls = savedSources.value
  if (!urls.length) return
  const rows = await probe(urls, true)
  if (!rows) return
  const okRows = rows.filter(r => !r.error && r.total)
  const total = okRows.reduce((a, r) => a + r.total, 0)
  const fails = rows.filter(r => r.error).length
  if (!total) { ElMessage.warning('同步源均未返回接口' + (fails ? `(${fails} 个拉取失败)` : '')); return }
  try {
    await ElMessageBox.confirm(
      `${okRows.length} 个同步源共 ${total} 个接口${fails ? `,另有 ${fails} 个来源拉取失败` : ''}。` +
      '同步将覆盖项目中同 method+path 的接口(含文档与 Mock 配置),确认执行?',
      '一键同步', { type: 'warning', confirmButtonText: '全部覆盖导入' })
  } catch { return }
  applying.value = true
  try {
    let created = 0, updated = 0, skipped = 0
    const allConflicts = []
    for (const r of okRows) {
      const gid = await resolveGroupId(r)
      const decisions = r.items.map(it => ({ method: it.method, path: it.path, action: 'overwrite' }))
      const d = await api.post(`/projects/${props.projectId}/import/openapi/apply`,
        { analysisId: r.analysisId, groupId: gid || undefined, decisions }, { projectId: props.projectId, timeout: 600000 })
      created += d.created; updated += d.updated; skipped += d.skipped
      if (d.conflicts?.length) allConflicts.push(...d.conflicts)
    }
    let msg = `新建 ${created},更新 ${updated},跳过 ${skipped}`
    if (allConflicts.length) msg += `,模板冲突 ${allConflicts.length}`
    ElMessage.success('同步完成:' + msg)
    results.value = null
    emit('changed')
  } catch (e) { ElMessage.error(e.message) } finally { applying.value = false }
}

async function analyze() {
  if (!content.value.trim()) { ElMessage.warning('请选择文件或粘贴内容'); return }
  analyzing.value = true
  plan.value = null
  try {
    const d = await api.post(`/projects/${props.projectId}/import/openapi/analyze`, { content: content.value },
      { projectId: props.projectId, timeout: 600000 })
    markActions(d)
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
      { content: content.value, groupId: targetGroupId.value > 0 ? targetGroupId.value : undefined, decisions }, { projectId: props.projectId, timeout: 600000 })
    let msg = `新建 ${d.created},更新 ${d.updated},跳过 ${d.skipped}`
    if (d.conflicts?.length) msg += `,模板冲突 ${d.conflicts.length}:${d.conflicts.join('、')}`
    ElMessage.success('导入完成:' + msg)
    plan.value = null
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
.row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.grow { flex: 1; }
.tip { color: #909399; font-size: 12px; margin-top: 6px; }
.method { font-weight: 700; }
.method.GET { color: #22c55e; }
.method.POST { color: #3b82f6; }
.method.PUT { color: #f59e0b; }
.method.DELETE { color: #ef4444; }
.method.PATCH { color: #a855f7; }
.mono { font-family: Consolas, Monaco, monospace; }
</style>
