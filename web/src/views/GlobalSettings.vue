<template>
  <div class="page">
    <header class="topbar">
      <div class="brand" @click="$router.push('/')">
        <el-icon><Back /></el-icon>&nbsp;{{ meta?.title || 'LiteApiDoc' }}
      </div>
      <div class="grow"></div>
      <el-tag type="success">管理员身份</el-tag>
    </header>

    <main class="main" v-if="auth.isAdmin">
      <el-alert type="warning" :closable="false" style="margin-bottom:16px"
        title="请定期备份数据文件(下方一键下载 .db 副本);数据仅保存在部署服务器本地" show-icon />

      <el-card header="基础设置">
        <el-form label-width="150px" style="max-width: 640px">
          <el-form-item label="平台标题"><el-input v-model="form.title" /></el-form-item>
          <el-form-item label="Logo 地址">
            <el-input v-model="form.logo" placeholder="图片 URL 或 data:image/…;base64,…" />
          </el-form-item>
          <el-form-item label="Mock 服务端口">
            <el-input v-model="form.mockPort" style="width: 160px" />
            <div class="tip">修改后需重启服务生效</div>
          </el-form-item>
          <el-form-item label="Mock CORS 白名单">
            <el-input v-model="form.mockCorsOrigins" placeholder="逗号分隔,如 http://dev.a.com,http://192.168.1.10:5173;留空=回显任意来源" />
            <div class="tip">限制允许浏览器直调 Mock 的前端来源;留空满足内网任意前端联调</div>
          </el-form-item>
          <el-form-item>
            <el-button type="primary" :loading="saving" @click="save">保存设置</el-button>
          </el-form-item>
        </el-form>
      </el-card>

      <el-card header="平台信息" style="margin-top:16px">
        <div class="kv"><span>版本</span><b>v{{ info.version }}</b></div>
        <div class="kv"><span>数据文件路径</span><b class="mono">{{ info.dbPath }}</b></div>
        <div class="kv"><span>Mock 服务端口(当前生效)</span><b>{{ info.mockPort }}</b></div>
        <el-button style="margin-top:12px" @click="downloadBackup"><el-icon><Download /></el-icon>&nbsp;下载 SQLite 备份(.db)</el-button>
      </el-card>

      <el-card header="危险操作" style="margin-top:16px">
        <el-button type="danger" plain @click="clearData">清空全部业务数据</el-button>
        <div class="tip">删除所有项目、接口、Mock 规则与日志,保留平台设置,不可恢复</div>
      </el-card>
    </main>

    <main class="main" v-else>
      <el-empty description="此页面需要管理员权限,请先登录">
        <el-button type="primary" @click="$router.push('/login')">前往登录</el-button>
      </el-empty>
    </main>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, useAuth } from '../store'

const auth = useAuth()
const meta = computed(() => auth.meta)
const info = ref({ version: '-', dbPath: '-', mockPort: '-' })
const saving = ref(false)
const form = reactive({ title: '', logo: '', mockPort: '', mockCorsOrigins: '' })

async function load() {
  await auth.loadMeta().catch(() => {})
  if (!auth.isAdmin) return
  const s = await api.get('/settings', { adminAuth: true }).catch(e => { ElMessage.error(e.message); return null })
  if (!s) return
  Object.assign(form, { title: s.title, logo: s.logo, mockPort: s.mockPort, mockCorsOrigins: s.mockCorsOrigins })
  info.value = { version: s.version, dbPath: s.dbPath, mockPort: s.mockPort }
}
onMounted(load)

async function save() {
  saving.value = true
  try {
    await api.put('/settings', form, { adminAuth: true })
    await auth.loadMeta()
    ElMessage.success('已保存' + (String(form.mockPort) !== String(info.value.mockPort) ? '(端口修改需重启生效)' : ''))
    info.value.mockPort = form.mockPort
  } catch (e) { ElMessage.error(e.message) } finally { saving.value = false }
}

async function downloadBackup() {
  try {
    const resp = await fetch('/api/admin/backup', { headers: { 'X-Auth-Token': auth.adminToken } })
    if (!resp.ok) throw new Error('下载失败')
    const blob = await resp.blob()
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `liteapidoc-backup-${new Date().toISOString().slice(0, 10)}.db`
    a.click()
    URL.revokeObjectURL(a.href)
  } catch (e) { ElMessage.error(e.message) }
}

async function clearData() {
  try {
    await ElMessageBox.prompt('此操作删除所有项目且不可恢复。请输入 YES 确认:', '清空全部业务数据',
      { type: 'warning', confirmButtonText: '下一步' })
  } catch { return }
  try {
    await api.post('/admin/clear', { confirm: true }, { adminAuth: true })
    ElMessage.success('已清空')
    load()
  } catch (e) { ElMessage.error(e.message) }
}
</script>

<style scoped>
.topbar { display: flex; align-items: center; padding: 0 24px; height: 56px; background: #fff; border-bottom: 1px solid #e4e7ed; }
.brand { display: flex; align-items: center; gap: 6px; cursor: pointer; font-weight: 600; }
.main { max-width: 860px; margin: 0 auto; padding: 20px 16px; }
.tip { color: #909399; font-size: 12px; margin-top: 4px; }
.kv { display: flex; gap: 12px; padding: 4px 0; font-size: 14px; }
.kv span { color: #909399; width: 190px; }
.mono { font-family: Consolas, monospace; }
</style>
