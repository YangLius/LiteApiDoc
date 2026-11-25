<template>
  <div class="page">
    <header class="topbar">
      <div class="left">
        <el-button text @click="$router.push('/')"><el-icon><Back /></el-icon></el-button>
        <span class="p-title">{{ project?.name || '…' }}</span>
        <el-tag v-if="project?.archived" size="small" type="warning">已归档(只读)</el-tag>
        <el-tag v-if="project?.hasPassword" size="small" type="info"><el-icon><Lock /></el-icon>&nbsp;已加密</el-tag>
        <el-tooltip :content="project?.mockEnabled ? 'Mock 服务已开启' : 'Mock 已关闭(Mock返回503)'">
          <span class="mock-dot" :class="{ off: !project?.mockEnabled }"></span>
        </el-tooltip>
      </div>
      <div class="right mono" v-if="project">
        <el-tooltip content="点击复制项目 Mock 基础地址">
          <el-button size="small" text @click="copyMockBase" class="mono">
            Mock: {{ mockBase }}
          </el-button>
        </el-tooltip>
      </div>
    </header>

    <div class="layout">
      <ApiTree
        :groups="groups" :apis="apis" :selected-id="selectedId"
        @select="onSelect"
        @create-api="gid => openEditor(null, gid)"
        @create-group="createGroup"
        @rename-group="renameGroup"
        @del-group="delGroup"
        @sort-groups="sortGroups"
        @move-api="moveApi"
      />

      <main class="content">
        <el-tabs v-model="tab">
          <el-tab-pane label="文档" name="doc">
            <DocPreview :api="currentApi" :project="project" :project-id="pid"
              @edit="openEditor(currentApi)" @debug="tab = 'debug'" @deleted="reload" />
          </el-tab-pane>
          <el-tab-pane label="调试" name="debug">
            <DebugPanel ref="debugRef" :project-id="pid" :project="project" :api="currentApi" :envs="envs"
              @manage-envs="tab = 'envs'" />
          </el-tab-pane>
          <el-tab-pane label="环境" name="envs">
            <EnvsPanel :project-id="pid" :envs="envs" @reload="loadEnvs" />
          </el-tab-pane>
          <el-tab-pane label="Mock 日志" name="logs" lazy>
            <LogsPanel ref="logsRef" :project-id="pid" />
          </el-tab-pane>
          <el-tab-pane label="导入导出" name="transfer" lazy>
            <TransferPanel :project-id="pid" :project-name="project?.name" :spec-url="project?.specUrl" @changed="reload" />
          </el-tab-pane>
          <el-tab-pane label="设置" name="settings" lazy>
            <SettingsPanel ref="settingsRef" :project-id="pid" :project="project"
              @reload="reloadAll" @need-unlock="showUnlock" />
          </el-tab-pane>
        </el-tabs>
      </main>
    </div>

    <!-- 接口编辑器 -->
    <ApiEditor v-model="editorVisible" :project-id="pid" :project="project" :api="editingApi"
      :default-group-id="editorGroupId" @saved="reload" />

    <!-- 项目解锁 -->
    <el-dialog v-model="unlockVisible" title="项目已开启访问密码" width="420px">
      <el-input v-model="unlockPwd" placeholder="项目访问密码" show-password @keyup.enter="doUnlock" />
      <template #footer>
        <el-button @click="unlockVisible = false">取消</el-button>
        <el-button type="primary" :loading="unlocking" @click="doUnlock">解锁</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, useAuth } from '../store'
import { copyText, mockUrl } from '../utils'
import ApiTree from '../components/ApiTree.vue'
import DocPreview from '../components/DocPreview.vue'
import ApiEditor from '../components/ApiEditor.vue'
import DebugPanel from '../components/DebugPanel.vue'
import EnvsPanel from '../components/EnvsPanel.vue'
import LogsPanel from '../components/LogsPanel.vue'
import TransferPanel from '../components/TransferPanel.vue'
import SettingsPanel from '../components/SettingsPanel.vue'

const route = useRoute()
const auth = useAuth()
const pid = Number(route.params.id)

const project = ref(null)
const groups = ref([])
const apis = ref([])
const envs = ref([])
const tab = ref('doc')
const selectedId = ref(0)
const editorVisible = ref(false)
const editingApi = ref(null)
const editorGroupId = ref(0)
const unlockVisible = ref(false)
const unlockPwd = ref('')
const unlocking = ref(false)
const logsRef = ref(null)

const currentApi = computed(() => apis.value.find(a => a.id === selectedId.value) || null)
const mockBase = computed(() => mockUrl(auth.meta, pid, '').replace(/\/$/, ''))

async function reload() {
  try {
    project.value = await api.get(`/projects/${pid}`, { projectId: pid })
  } catch (e) {
    if (e.status === 404) { ElMessage.error('项目不存在'); return }
  }
  await loadTree()
  await loadEnvs()
}
const reloadAll = reload

async function loadTree() {
  const [g, a] = await Promise.all([
    api.get(`/projects/${pid}/groups`, { projectId: pid }).catch(() => []),
    api.get(`/projects/${pid}/apis`, { projectId: pid }).catch(() => [])
  ])
  groups.value = g || []
  apis.value = a || []
  if (selectedId.value && !apis.value.find(x => x.id === selectedId.value)) selectedId.value = 0
}
async function loadEnvs() {
  envs.value = await api.get(`/projects/${pid}/envs`, { projectId: pid }).catch(() => [])
}
onMounted(reload)

watch(tab, t => { if (t === 'logs') logsRef.value?.load() })

function onSelect(id) { selectedId.value = id; if (tab.value !== 'doc' && tab.value !== 'debug') tab.value = 'doc' }

function openEditor(apiObj, groupId) {
  if (project.value?.archived) { ElMessage.warning('项目已归档,处于只读状态'); return }
  editingApi.value = apiObj
  editorGroupId.value = Number(groupId) || 0
  editorVisible.value = true
}

async function createGroup(payload) {
  try {
    await api.post(`/projects/${pid}/groups`, payload, { projectId: pid })
    loadTree()
  } catch (e) { handleErr(e) }
}
async function renameGroup(payload) {
  try {
    await api.patch(`/groups/${payload.id}`, { name: payload.name }, { projectId: pid })
    loadTree()
  } catch (e) { handleErr(e) }
}
async function delGroup(id) {
  try {
    await api.delete(`/groups/${id}`, { projectId: pid })
    loadTree()
  } catch (e) { handleErr(e) }
}
async function sortGroups(items) {
  try {
    await api.post(`/projects/${pid}/groups/sort`, items, { projectId: pid })
    loadTree()
  } catch (e) { handleErr(e) }
}
async function moveApi(items) {
  try {
    await api.post(`/projects/${pid}/apis/sort`, items, { projectId: pid })
    loadTree()
  } catch (e) { handleErr(e) }
}

async function copyMockBase() {
  await copyText(mockBase.value + '/')
  ElMessage.success('Mock 基础地址已复制')
}

function showUnlock() { unlockPwd.value = ''; unlockVisible.value = true }

async function doUnlock() {
  if (!unlockPwd.value) return
  unlocking.value = true
  try {
    const d = await api.post(`/projects/${pid}/unlock`, { password: unlockPwd.value })
    auth.setProjectToken(pid, d.token)
    unlockVisible.value = false
    ElMessage.success('已解锁')
    reload()
  } catch (e) {
    ElMessage.error(e.message)
  } finally { unlocking.value = false }
}

function handleErr(e) {
  if (e.needUnlock) showUnlock()
  else ElMessage.error(e.message)
}
</script>

<style scoped>
.page { min-height: 100vh; }
.topbar { display: flex; align-items: center; justify-content: space-between; padding: 0 16px; height: 56px; background: #fff; border-bottom: 1px solid #e4e7ed; }
.left { display: flex; align-items: center; gap: 10px; }
.p-title { font-size: 17px; font-weight: 700; }
.mock-dot { width: 10px; height: 10px; border-radius: 50%; background: #22c55e; display: inline-block; }
.mock-dot.off { background: #c0c4cc; }
.right { display: flex; align-items: center; }
.layout { display: flex; }
.content { flex: 1; min-width: 0; }
.content :deep(.el-tabs__header) { background: #fff; margin: 0; padding: 0 16px; }
.mono { font-family: Consolas, Monaco, monospace; }
</style>
