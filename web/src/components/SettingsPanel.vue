<template>
  <div class="panel" v-if="project">
    <div class="sec">
      <h4>基础信息</h4>
      <el-form label-width="100px" style="max-width: 560px">
        <el-form-item label="项目名称"><el-input v-model="form.name" /></el-form-item>
        <el-form-item label="项目描述"><el-input v-model="form.description" type="textarea" :rows="2" /></el-form-item>
        <el-form-item label="标签">
          <el-select v-model="form.tags" multiple filterable allow-create default-first-option style="width:100%" />
        </el-form-item>
        <el-form-item label="Mock 服务">
          <el-switch v-model="form.mockEnabled" active-text="开启" inactive-text="关闭(所有Mock返回503)" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" size="small" @click="saveBasic">保存</el-button>
        </el-form-item>
      </el-form>
    </div>

    <div class="sec">
      <h4>访问密码</h4>
      <div class="row">
        <template v-if="project.hasPassword">
          <el-tag type="warning">已开启</el-tag>
          <el-button size="small" @click="setPwd('')">关闭密码(内网信任模式)</el-button>
        </template>
        <template v-else>
          <el-tag type="info">未开启(所有访问者均可编辑)</el-tag>
          <el-button size="small" @click="setPwdFromInput">开启密码</el-button>
        </template>
        <el-button size="small" type="warning" plain @click="resetPwd">管理员重置密码</el-button>
      </div>
      <div class="tip">密码连续输错 5 次将锁定 10 分钟;密码丢失由管理员凭平台管理密码重置</div>
    </div>

    <div class="sec">
      <h4>分享链接(只读)</h4>
      <div class="row">
        <el-switch v-model="share.enabled" @change="saveShare" active-text="开启" />
        <template v-if="share.token">
          <el-input :model-value="shareUrl" readonly size="small" class="mono share-input">
            <template #append><el-button @click="copyShare">复制</el-button></template>
          </el-input>
        </template>
      </div>
      <div class="row" style="margin-top:8px">
        <span class="lbl">访问密码:</span>
        <el-tag v-if="share.hasPassword" size="small" type="warning">已设置</el-tag>
        <el-tag v-else size="small" type="info">无</el-tag>
        <el-button size="small" text @click="setSharePwd">{{ share.hasPassword ? '修改密码' : '设置密码' }}</el-button>
        <el-button size="small" text @click="saveShare({ resetToken: true })">重置链接(旧链接失效)</el-button>
        <el-button size="small" text type="danger" @click="revokeShare">彻底删除分享</el-button>
      </div>
      <div class="tip">分享链接仅可查看文档与访问 Mock,不能编辑;可随时失效</div>
    </div>

    <div class="sec">
      <h4>归档</h4>
      <div class="row">
        <el-switch :model-value="project.archived" @change="toggleArchive"
          active-text="已归档(只读)" inactive-text="未归档" />
      </div>
    </div>

    <div class="sec danger-sec">
      <h4>删除项目</h4>
      <el-button type="danger" @click="delProject">删除本项目(不可恢复)</el-button>
      <div class="tip">接口、Mock 规则、日志全部清除;需要管理员权限</div>
    </div>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useRouter } from 'vue-router'
import { api, useAuth } from '../store'
import { copyText } from '../utils'

const props = defineProps({ projectId: { type: Number, required: true } })
const emit = defineEmits(['reload', 'changed'])
const router = useRouter()
const auth = useAuth()

const project = computed(() => props.project || null)
const form = reactive({ name: '', description: '', tags: [], mockEnabled: true })
const share = reactive({ token: '', enabled: false, hasPassword: false })

watch(project, p => {
  if (!p) return
  form.name = p.name; form.description = p.description
  form.tags = [...(p.tags || [])]; form.mockEnabled = p.mockEnabled
}, { immediate: true })

async function loadShare() {
  try {
    const s = await api.get(`/projects/${props.projectId}/share`, { projectId: props.projectId })
    Object.assign(share, { token: s.token || '', enabled: !!s.enabled, hasPassword: !!s.hasPassword })
  } catch { Object.assign(share, { token: '', enabled: false, hasPassword: false }) }
}
loadShare()

async function saveBasic() {
  try {
    await api.patch(`/projects/${props.projectId}`, form, { projectId: props.projectId })
    ElMessage.success('已保存')
    emit('reload')
  } catch (e) { handleErr(e) }
}

async function setPwd(pwd) {
  try {
    await api.put(`/projects/${props.projectId}/password`, { password: pwd }, { projectId: props.projectId })
    auth.setProjectToken(props.projectId, pwd ? auth.projectToken(props.projectId) : '')
    ElMessage.success(pwd ? '访问密码已开启' : '访问密码已关闭')
    emit('reload')
  } catch (e) { handleErr(e) }
}
async function setPwdFromInput() {
  try {
    const { value } = await ElMessageBox.prompt('设置项目访问密码(编辑该项目需先解锁)', '开启访问密码',
      { inputPattern: /\S{4,}/, inputErrorMessage: '密码至少 4 位', inputType: 'password' })
    await setPwd(value)
    ElMessage.info('已在浏览器记住解锁状态')
  } catch {}
}
async function resetPwd() {
  try {
    const { value } = await ElMessageBox.prompt('输入新的项目访问密码(留空清除)', '管理员重置密码',
      { inputType: 'password' })
    await api.put(`/projects/${props.projectId}/password`, { password: value || '' }, { adminAuth: true })
    auth.setProjectToken(props.projectId, '')
    ElMessage.success('已重置')
    emit('reload')
  } catch {}
}

const shareUrl = computed(() => share.token ? `${location.origin}/s/${share.token}` : '')
async function saveShare(extra = {}) {
  try {
    const d = await api.put(`/projects/${props.projectId}/share`,
      { enabled: share.enabled, resetToken: !!extra.resetToken }, { projectId: props.projectId })
    share.token = d.token; share.enabled = d.enabled; share.hasPassword = d.hasPassword
    ElMessage.success('分享设置已更新')
  } catch (e) { handleErr(e) }
}
async function setSharePwd() {
  try {
    const { value } = await ElMessageBox.prompt('分享访问密码(留空清除)', '分享密码',
      { inputType: 'password' })
    const d = await api.put(`/projects/${props.projectId}/share`,
      { enabled: share.enabled, password: value || '', clearPassword: !value }, { projectId: props.projectId })
    Object.assign(share, { token: d.token, enabled: d.enabled, hasPassword: d.hasPassword })
    ElMessage.success('已更新')
  } catch {}
}
async function revokeShare() {
  try { await ElMessageBox.confirm('删除分享配置,已分发的链接立即失效。确认?', '删除分享', { type: 'warning' }) } catch { return }
  try {
    await api.delete(`/projects/${props.projectId}/share`, { projectId: props.projectId })
    Object.assign(share, { token: '', enabled: false, hasPassword: false })
    ElMessage.success('已删除')
  } catch (e) { handleErr(e) }
}
async function copyShare() {
  await copyText(shareUrl.value)
  ElMessage.success('分享链接已复制')
}

async function toggleArchive(v) {
  try {
    await api.post(`/projects/${props.projectId}/archive`, { archived: !!v }, { projectId: props.projectId })
    emit('reload')
  } catch (e) { handleErr(e) }
}

async function delProject() {
  try {
    await ElMessageBox.confirm('接口、Mock 规则、日志全部清除且不可恢复,确认删除?', '删除项目',
      { type: 'warning', confirmButtonText: '删除' })
  } catch { return }
  try {
    await api.delete(`/projects/${props.projectId}`, { adminAuth: true })
    ElMessage.success('已删除')
    router.push('/')
  } catch (e) { handleErr(e) }
}

function handleErr(e) {
  if (e.needUnlock) emit('needUnlock', e)
  else ElMessage.error(e.message)
}
defineExpose({ loadShare })
</script>

<style scoped>
.panel { padding: 14px; }
.sec { margin-bottom: 24px; }
.sec h4 { margin: 0 0 10px; border-left: 3px solid #409eff; padding-left: 8px; }
.row { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; }
.lbl { font-size: 13px; color: #606266; }
.share-input { max-width: 480px; }
.tip { color: #909399; font-size: 12px; margin-top: 6px; }
.danger-sec h4 { border-left-color: #f56c6c; }
</style>
