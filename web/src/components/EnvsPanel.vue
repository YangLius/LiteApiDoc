<template>
  <div class="panel">
    <div class="bar">
      <el-button size="small" type="primary" plain @click="openAdd"><el-icon><Plus /></el-icon>&nbsp;新建环境</el-button>
      <span class="tip">调试面板可切换 Mock 环境或任一自定义环境</span>
    </div>
    <el-table :data="envs" size="small">
      <el-table-column prop="name" label="名称" width="160" />
      <el-table-column prop="baseUrl" label="Base URL" class-name="mono" />
      <el-table-column label="操作" width="130">
        <template #default="{ row }">
          <el-button size="small" text @click="openEdit(row)">编辑</el-button>
          <el-button size="small" text type="danger" @click="del(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dlg" :title="editForm.id ? '编辑环境' : '新建环境'" width="460px">
      <el-form label-width="90px">
        <el-form-item label="名称" required><el-input v-model="editForm.name" placeholder="dev / test" /></el-form-item>
        <el-form-item label="Base URL" required>
          <el-input v-model="editForm.baseUrl" placeholder="http://192.168.1.10:8080" class="mono" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dlg = false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '../store'

const props = defineProps({
  projectId: { type: Number, required: true },
  envs: { type: Array, default: () => [] }
})
const emit = defineEmits(['reload'])
const dlg = ref(false)
const editForm = reactive({ id: 0, name: '', baseUrl: '' })

function openAdd() {
  editForm.id = 0; editForm.name = ''; editForm.baseUrl = ''
  dlg.value = true
}
function openEdit(row) {
  editForm.id = row.id; editForm.name = row.name; editForm.baseUrl = row.baseUrl
  dlg.value = true
}
async function save() {
  if (!editForm.name.trim() || !editForm.baseUrl.trim()) { ElMessage.warning('请填写名称与 Base URL'); return }
  try {
    if (editForm.id) await api.patch(`/envs/${editForm.id}`, { name: editForm.name, baseUrl: editForm.baseUrl }, { projectId: props.projectId })
    else await api.post(`/projects/${props.projectId}/envs`, { name: editForm.name, baseUrl: editForm.baseUrl }, { projectId: props.projectId })
    dlg.value = false
    emit('reload')
  } catch (e) { ElMessage.error(e.message) }
}
async function del(row) {
  try { await ElMessageBox.confirm(`删除环境「${row.name}」?`, '删除', { type: 'warning' }) } catch { return }
  try {
    await api.delete(`/envs/${row.id}`, { projectId: props.projectId })
    emit('reload')
  } catch (e) { ElMessage.error(e.message) }
}
</script>

<style scoped>
.panel { padding: 14px; }
.bar { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.tip { color: #909399; font-size: 12px; }
</style>
