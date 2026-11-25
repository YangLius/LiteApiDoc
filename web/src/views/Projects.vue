<template>
  <div class="page">
    <header class="topbar">
      <div class="brand" @click="$router.push('/')">
        <img v-if="meta?.logo" :src="meta.logo" class="logo" alt="">
        <span class="brand-name">{{ meta?.title || 'LiteApiDoc' }}</span>
        <span class="ver">v{{ meta?.version }}</span>
      </div>
      <div class="actions">
        <el-button text @click="$router.push('/settings')"><el-icon><Setting /></el-icon>&nbsp;平台设置</el-button>
        <el-button v-if="auth.isAdmin" text type="danger" @click="logoutAdmin">退出管理身份</el-button>
        <el-button v-else text @click="openAdmin('login')"><el-icon><User /></el-icon>&nbsp;管理员登录</el-button>
      </div>
    </header>

    <main class="main">
      <div class="toolbar">
        <el-input v-model="kw" placeholder="搜索项目名称 / 标签" clearable style="width: 280px">
          <template #prefix><el-icon><Search /></el-icon></template>
        </el-input>
        <div class="grow"></div>
        <el-button @click="importVisible = true"><el-icon><Upload /></el-icon>&nbsp;导入项目备份</el-button>
        <el-button type="primary" @click="openCreate"><el-icon><Plus /></el-icon>&nbsp;新建项目</el-button>
      </div>

      <el-empty v-if="!filtered.length" description="还没有项目,点击右上角「新建项目」开始" />

      <div class="cards">
        <div v-for="p in filtered" :key="p.id" class="card" @click="$router.push('/p/' + p.id)">
          <div class="card-head">
            <span class="p-name">{{ p.name }}</span>
            <el-tag v-if="p.archived" size="small" type="warning">已归档</el-tag>
            <el-tooltip :content="p.mockEnabled ? 'Mock 服务已开启' : 'Mock 服务已关闭'">
              <span class="mock-dot" :class="{ off: !p.mockEnabled }"></span>
            </el-tooltip>
          </div>
          <div class="p-desc">{{ p.description || '暂无描述' }}</div>
          <div class="p-tags">
            <el-tag v-for="t in p.tags" :key="t" size="small" effect="plain">{{ t }}</el-tag>
          </div>
          <div class="card-foot">
            <span>{{ p.apiCount }} 个接口</span>
            <span>更新于 {{ fmtTime(p.updatedAt) }}</span>
            <span class="grow"></span>
            <el-button size="small" text @click.stop="toggleArchive(p)">
              {{ p.archived ? '取消归档' : '归档' }}
            </el-button>
            <el-button v-if="auth.isAdmin" size="small" text type="danger" @click.stop="delProject(p)">删除</el-button>
          </div>
        </div>
      </div>
    </main>

    <!-- 新建项目 -->
    <el-dialog v-model="createVisible" title="新建项目" width="520px">
      <el-form label-width="110px">
        <el-form-item label="项目名称" required><el-input v-model="form.name" maxlength="50" /></el-form-item>
        <el-form-item label="项目描述"><el-input v-model="form.description" type="textarea" :rows="2" /></el-form-item>
        <el-form-item label="标签">
          <el-select v-model="form.tags" multiple filterable allow-create default-first-option
            placeholder="输入后回车添加" style="width:100%" />
        </el-form-item>
        <el-form-item label="访问密码">
          <el-input v-model="form.password" placeholder="可选;设置后编辑需先解锁" show-password clearable />
          <div class="tip">不设置 = 内网信任模式,所有访问者均可编辑</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="doCreate">创建</el-button>
      </template>
    </el-dialog>

    <!-- 平台管理密码:首次设置 / 登录 -->
    <el-dialog v-model="adminDlgVisible" :title="adminMode === 'setup' ? '设置平台管理密码' : '管理员登录'" width="420px">
      <el-form>
        <el-form-item :label="adminMode === 'setup' ? '设置密码' : '管理密码'">
          <el-input v-model="adminPwd" show-password @keyup.enter="doAdmin" />
          <div class="tip" v-if="adminMode === 'setup'">首次启动设置;用于新建/删除项目、全局设置等管理操作</div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="adminDlgVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="doAdmin">确定</el-button>
      </template>
    </el-dialog>

    <!-- 导入项目备份 -->
    <el-dialog v-model="importVisible" title="导入项目备份(平台 JSON)" width="560px">
      <el-alert type="info" :closable="false" style="margin-bottom:12px"
        title="导入将创建一个新项目;需要管理员权限" />
      <input type="file" accept=".json" @change="onPickFile" style="margin-bottom:10px" />
      <el-input v-model="importContent" type="textarea" :rows="8" placeholder="或直接粘贴备份 JSON 内容" />
      <template #footer>
        <el-button @click="importVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="doImport">导入</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, useAuth } from '../store'
import { fmtTime } from '../utils'

const router = useRouter()
const auth = useAuth()
const meta = computed(() => auth.meta)
const kw = ref('')
const projects = ref([])
const saving = ref(false)
const createVisible = ref(false)
const adminDlgVisible = ref(false)
const adminMode = ref('login') // setup | login
const adminPwd = ref('')
const importVisible = ref(false)
const importContent = ref('')
const form = reactive({ name: '', description: '', tags: [], password: '' })
let pendingAfterAdmin = null

const filtered = computed(() => {
  const k = kw.value.trim().toLowerCase()
  if (!k) return projects.value
  return projects.value.filter(p =>
    p.name.toLowerCase().includes(k) || (p.tags || []).some(t => t.toLowerCase().includes(k)))
})

async function load() {
  await auth.loadMeta().catch(() => {})
  projects.value = await api.get('/projects').catch(() => [])
}
onMounted(load)

function logoutAdmin() {
  auth.setAdminToken('')
  ElMessage.success('已退出管理身份')
}

function openAdmin(mode, after) {
  adminMode.value = mode
  adminPwd.value = ''
  pendingAfterAdmin = after || null
  adminDlgVisible.value = true
}

async function doAdmin() {
  if (!adminPwd.value) return
  saving.value = true
  try {
    const data = adminMode.value === 'setup'
      ? await api.post('/auth/admin/setup', { password: adminPwd.value })
      : await api.post('/auth/admin/login', { password: adminPwd.value })
    auth.setAdminToken(data.token)
    await auth.loadMeta()
    adminDlgVisible.value = false
    ElMessage.success(adminMode.value === 'setup' ? '平台管理密码已设置' : '已获得管理员权限')
    if (pendingAfterAdmin) { const fn = pendingAfterAdmin; pendingAfterAdmin = null; fn() }
  } catch (e) {
    ElMessage.error(e.message)
  } finally { saving.value = false }
}

function openCreate() {
  form.name = ''; form.description = ''; form.tags = []; form.password = ''
  if (!meta.value?.adminInitialized) { openAdmin('setup', () => { createVisible.value = true }) }
  else if (!auth.isAdmin) { openAdmin('login', () => { createVisible.value = true }) }
  else createVisible.value = true
}

async function doCreate() {
  if (!form.name.trim()) { ElMessage.warning('请输入项目名称'); return }
  saving.value = true
  try {
    const p = await api.post('/projects', form, { adminAuth: true })
    createVisible.value = false
    ElMessage.success('项目已创建')
    router.push('/p/' + p.id)
  } catch (e) {
    if (e.needUnlock) openAdmin('login', () => doCreate())
    else ElMessage.error(e.message)
  } finally { saving.value = false }
}

async function toggleArchive(p) {
  try {
    await api.post(`/projects/${p.id}/archive`, { archived: !p.archived }, { projectId: p.id })
    p.archived = !p.archived
    ElMessage.success(p.archived ? '已归档(只读)' : '已取消归档')
  } catch (e) {
    if (e.needUnlock) ElMessage.info('该项目已开启访问密码,请在项目页解锁后操作')
    else ElMessage.error(e.message)
  }
}

async function delProject(p) {
  try {
    await ElMessageBox.confirm(`删除「${p.name}」后接口、Mock 规则、日志全部清除且不可恢复,确认删除?`,
      '删除项目', { type: 'warning', confirmButtonText: '删除', confirmButtonClass: 'el-button--danger' })
  } catch { return }
  try {
    await api.delete(`/projects/${p.id}`, { adminAuth: true })
    ElMessage.success('已删除')
    load()
  } catch (e) {
    if (e.needUnlock) openAdmin('login', () => delProject(p))
    else ElMessage.error(e.message)
  }
}

function onPickFile(ev) {
  const f = ev.target.files?.[0]
  if (!f) return
  const reader = new FileReader()
  reader.onload = () => { importContent.value = String(reader.result || '') }
  reader.readAsText(f)
}

async function doImport() {
  if (!importContent.value.trim()) { ElMessage.warning('请选择或粘贴备份内容'); return }
  saving.value = true
  try {
    const data = await api.post('/admin/import/project', { content: importContent.value }, { adminAuth: true })
    importVisible.value = false
    importContent.value = ''
    ElMessage.success(`导入成功,新项目 #${data.projectId}` + (data.warnings?.length ? `,${data.warnings.length} 条警告` : ''))
    if (data.warnings?.length) console.warn('导入警告:', data.warnings)
    load()
  } catch (e) {
    if (e.needUnlock) openAdmin('login', () => doImport())
    else ElMessage.error(e.message)
  } finally { saving.value = false }
}
</script>

<style scoped>
.page { min-height: 100vh; }
.topbar { display: flex; align-items: center; justify-content: space-between; padding: 0 24px; height: 56px; background: #fff; border-bottom: 1px solid #e4e7ed; }
.brand { display: flex; align-items: center; gap: 8px; cursor: pointer; }
.brand-name { font-size: 18px; font-weight: 700; }
.ver { color: #909399; font-size: 12px; }
.logo { height: 24px; }
.actions { display: flex; align-items: center; }
.main { max-width: 1100px; margin: 0 auto; padding: 20px 16px; }
.toolbar { display: flex; gap: 12px; align-items: center; margin-bottom: 18px; }
.grow { flex: 1; }
.cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 14px; }
.card { background: #fff; border: 1px solid #e4e7ed; border-radius: 8px; padding: 14px 16px; cursor: pointer; transition: box-shadow .2s; }
.card:hover { box-shadow: 0 4px 14px rgba(0,0,0,.08); }
.card-head { display: flex; align-items: center; gap: 8px; }
.p-name { font-weight: 600; font-size: 15px; flex: 1; }
.mock-dot { width: 9px; height: 9px; border-radius: 50%; background: #22c55e; display: inline-block; }
.mock-dot.off { background: #c0c4cc; }
.p-desc { color: #909399; font-size: 13px; margin: 6px 0; min-height: 18px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.p-tags { display: flex; gap: 6px; flex-wrap: wrap; min-height: 8px; }
.card-foot { display: flex; align-items: center; gap: 10px; color: #909399; font-size: 12px; margin-top: 10px; }
.tip { color: #909399; font-size: 12px; line-height: 1.4; margin-top: 4px; }
</style>
