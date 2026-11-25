<template>
  <div class="debug">
    <div class="req-side">
      <div class="env-bar">
        <el-select v-model="envKey" size="small" style="width: 220px">
          <el-option label="Mock 环境" value="__mock" />
          <el-option v-for="e in envs" :key="e.id" :value="'env' + e.id" :label="e.name + '(' + e.baseUrl + ')'" />
        </el-select>
        <el-button size="small" text @click="$emit('manageEnvs')"><el-icon><Setting /></el-icon>管理环境</el-button>
      </div>

      <div class="req-line">
        <el-select v-model="req.method" style="width: 100px" size="small">
          <el-option v-for="m in ['GET','POST','PUT','DELETE','PATCH']" :key="m" :value="m" :label="m" />
        </el-select>
        <el-input v-model="req.path" size="small" placeholder="/users/1" class="mono" @keyup.enter="send">
          <template #prepend>{{ targetPrefix }}</template>
        </el-input>
        <el-button type="primary" size="small" :loading="sending" @click="send">发送</el-button>
      </div>

      <el-tabs v-model="tab" size="small">
        <el-tab-pane label="Query" name="q">
          <div v-for="(q, i) in req.query" :key="i" class="kv-row">
            <el-input v-model="q.key" size="small" placeholder="参数名" class="mono" />
            <el-input v-model="q.value" size="small" placeholder="值" class="mono" />
            <el-button size="small" text type="danger" @click="req.query.splice(i, 1)"><el-icon><Delete /></el-icon></el-button>
          </div>
          <el-button size="small" text @click="req.query.push({ key: '', value: '' })"><el-icon><Plus /></el-icon>添加</el-button>
        </el-tab-pane>
        <el-tab-pane label="Header" name="h">
          <div v-for="(h, i) in req.headers" :key="i" class="kv-row">
            <el-input v-model="h.key" size="small" placeholder="Key" class="mono" />
            <el-input v-model="h.value" size="small" placeholder="Value" class="mono" />
            <el-button size="small" text type="danger" @click="req.headers.splice(i, 1)"><el-icon><Delete /></el-icon></el-button>
          </div>
          <el-button size="small" text @click="req.headers.push({ key: '', value: '' })"><el-icon><Plus /></el-icon>添加</el-button>
          <div class="tip">已自动带入接口 Header 与项目全局 Header(同名可手动覆盖)</div>
        </el-tab-pane>
        <el-tab-pane label="Body" name="b">
          <el-input v-model="req.body" type="textarea" :rows="8" class="mono"
            placeholder='{"id": 1}' :disabled="req.method === 'GET' || req.method === 'DELETE'" />
        </el-tab-pane>
      </el-tabs>
    </div>

    <div class="resp-side">
      <template v-if="resp">
        <div class="resp-status">
          <el-tag :type="respStatusType" size="large">{{ resp.status || 'ERR' }}</el-tag>
          <span v-if="resp.error" class="err-text">{{ resp.error }}</span>
          <span class="grow"></span>
          <span>耗时 {{ resp.durationMs }}ms · {{ resp.size }} bytes</span>
          <el-button size="small" text @click="copyAsCurl"><el-icon><CopyDocument /></el-icon>复制请求</el-button>
        </div>
        <el-tabs v-model="respTab" size="small">
          <el-tab-pane label="响应体" name="body"><pre class="code-block">{{ resp.body || '(空)' }}</pre></el-tab-pane>
          <el-tab-pane label="响应头" name="headers">
            <table class="hdr"><tr v-for="(vs, k) in resp.headers" :key="k"><td class="mono">{{ k }}</td><td class="mono">{{ vs.join(', ') }}</td></tr></table>
          </el-tab-pane>
        </el-tabs>
      </template>
      <el-empty v-else description="发送请求查看响应" :image-size="70" />
    </div>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { api, useAuth } from '../store'
import { mockUrl, copyText, prettyJSON } from '../utils'

const props = defineProps({
  projectId: { type: Number, required: true },
  project: { type: Object, default: null },
  api: { type: Object, default: null },
  envs: { type: Array, default: () => [] }
})
defineEmits(['manageEnvs'])
const auth = useAuth()

const envKey = ref('__mock')
const tab = ref('q')
const respTab = ref('body')
const sending = ref(false)
const resp = ref(null)
const req = reactive({ method: 'GET', path: '', query: [], headers: [], body: '' })

const targetPrefix = computed(() => {
  if (envKey.value === '__mock') return mockUrl(auth.meta, props.projectId, '').replace(/\/$/, '')
  const e = props.envs.find(x => 'env' + x.id === envKey.value)
  return (e?.baseUrl || '').replace(/\/$/, '')
})

// 选中接口时自动带入参数(PRD 模块5)
watch(() => props.api, a => {
  if (!a) return
  req.method = a.method
  let path = a.path
  const qp = []
  for (const p of (a.pathParams || [])) path = path.replace(`{${p.name}}`, p.example || p.name)
  for (const p of (a.queryParams || [])) qp.push({ key: p.name, value: p.example || '' })
  req.path = path
  req.query = qp
  const hs = []
  for (const h of (props.project?.globalHeaders || [])) if (h.key) hs.push({ key: h.key, value: h.value })
  for (const h of (a.headerParams || [])) {
    const exist = hs.find(x => x.key.toLowerCase() === h.key.toLowerCase())
    if (exist && !h.default) continue
    if (exist) exist.value = h.value || exist.value
    else if (h.key && h.default) hs.push({ key: h.key, value: h.value })
  }
  req.headers = hs
  req.body = (a.method !== 'GET' && a.method !== 'DELETE') ? (a.bodyExample || '') : ''
}, { immediate: true })

const respStatusType = computed(() => {
  const s = resp.value?.status || 0
  if (!s) return 'info'
  if (s < 300) return 'success'
  if (s < 400) return 'primary'
  if (s < 500) return 'warning'
  return 'danger'
})

async function send() {
  if (!req.path.startsWith('/')) req.path = '/' + req.path
  const qs = req.query.filter(x => x.key).map(x => `${encodeURIComponent(x.key)}=${encodeURIComponent(x.value)}`).join('&')
  sending.value = true
  resp.value = null
  try {
    const d = await api.post('/debug', {
      projectId: props.projectId,
      method: req.method,
      url: targetPrefix.value + req.path + (qs ? '?' + qs : ''),
      headers: Object.fromEntries(req.headers.filter(h => h.key).map(h => [h.key, h.value])),
      body: (req.method !== 'GET' && req.method !== 'DELETE') ? req.body : ''
    }, { projectId: props.projectId })
    resp.value = d
    resp.value.body = prettyJSON(d.body)
  } catch (e) {
    ElMessage.error(e.message)
  } finally { sending.value = false }
}

async function copyAsCurl() {
  if (!resp.value?.finalUrl) return
  let c = `curl -X ${req.method} "${resp.value.finalUrl}"`
  for (const [k, v] of Object.entries(req.headers)) {}
  const hdrs = req.headers.filter(h => h.key).map(h => `  -H "${h.key}: ${h.value}"`)
  if (hdrs.length) c += ' \\\n' + hdrs.join(' \\\n')
  if (req.body && req.method !== 'GET') c += ` \\\n  -d '${req.body.replace(/'/g, '')}'`
  await copyText(c)
  ElMessage.success('已复制')
}
</script>

<style scoped>
.debug { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; height: calc(100vh - 170px); padding: 14px; }
.req-side, .resp-side { background: #fff; border: 1px solid #e4e7ed; border-radius: 8px; padding: 12px; overflow: auto; }
.env-bar { display: flex; align-items: center; gap: 8px; margin-bottom: 10px; }
.req-line { display: flex; gap: 8px; margin-bottom: 10px; }
.kv-row { display: grid; grid-template-columns: 1fr 1.4fr 32px; gap: 6px; margin-bottom: 6px; }
.tip { color: #909399; font-size: 12px; margin-top: 8px; }
.resp-status { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; font-size: 13px; color: #606266; }
.grow { flex: 1; }
.err-text { color: #f56c6c; }
.hdr { border-collapse: collapse; font-size: 12px; width: 100%; }
.hdr td { border: 1px solid #ebeef5; padding: 4px 8px; }
.mono :deep(textarea), .mono :deep(input) { font-family: Consolas, monospace; }
</style>
