<template>
  <el-dialog v-model="visible" :title="isEdit ? '编辑接口' : '新建接口'" fullscreen :close-on-click-modal="false"
    @open="onOpen" @closed="$emit('closed')">
    <div class="editor" v-if="form">
      <el-form label-width="92px" class="basic">
        <el-row :gutter="12">
          <el-col :span="8"><el-form-item label="接口名称" required><el-input v-model="form.name" maxlength="60" /></el-form-item></el-col>
          <el-col :span="5">
            <el-form-item label="请求方式">
              <el-select v-model="form.method">
                <el-option v-for="m in ['GET','POST','PUT','DELETE','PATCH']" :key="m" :value="m" :label="m" />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :span="8">
            <el-form-item label="接口路径" required>
              <el-input v-model="form.path" placeholder="/users/{id}" class="mono">
                <template #prepend>/</template>
              </el-input>
            </el-form-item>
          </el-col>
          <el-col :span="3">
            <el-form-item label-width="0">
              <el-radio-group v-model="form.status">
                <el-radio-button v-for="s in statusList" :key="s.value" :value="s.value">{{ s.label }}</el-radio-button>
              </el-radio-group>
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="12">
          <el-col :span="8"><el-form-item label="负责人"><el-input v-model="form.owner" /></el-form-item></el-col>
          <el-col :span="16"><el-form-item label="备注"><el-input v-model="form.remark" /></el-form-item></el-col>
        </el-row>
      </el-form>

      <el-divider content-position-left>Path 参数(路径中 {x} 占位)</el-divider>
      <ParamTable :rows="form.pathParams" kind="path" />

      <el-divider content-position-left>Query 参数</el-divider>
      <ParamTable :rows="form.queryParams" kind="query" />

      <el-divider content-position-left>Header 参数</el-divider>
      <div class="hd-table">
        <div class="hd-row hd-head"><span>Key</span><span>示例值</span><span>备注</span><span>调试默认携带</span><span></span></div>
        <div class="hd-row" v-for="(h, i) in form.headerParams" :key="i">
          <el-input v-model="h.key" size="small" placeholder="X-Token" class="mono" />
          <el-input v-model="h.value" size="small" />
          <el-input v-model="h.remark" size="small" />
          <el-checkbox v-model="h.default" size="small" />
          <el-button size="small" text type="danger" @click="form.headerParams.splice(i, 1)"><el-icon><Delete /></el-icon></el-button>
        </div>
        <el-button size="small" text type="primary" @click="addHeader"><el-icon><Plus /></el-icon>&nbsp;添加 Header</el-button>
        <div v-if="project?.globalHeaders?.length" class="global-tip">
          项目全局 Header:{{ project.globalHeaders.map(h => h.key).join(', ') }}(调试时自动携带,同名时手动输入优先)
        </div>
      </div>

      <el-divider content-position-left>Body(application/json)</el-divider>
      <div class="body-sec">
        <SchemaEditor v-model="form.bodySchema" @gen="genBodyExample" />
        <div class="body-example">
          <div class="be-head">
            <b>Body 示例</b>
            <el-button size="small" text type="primary" :loading="genning" @click="genBodyExample">
              <el-icon><MagicStick /></el-icon>&nbsp;根据 Schema 生成
            </el-button>
          </div>
          <el-input v-model="form.bodyExample" type="textarea" :rows="6" class="mono" placeholder='{"id": 1}' />
        </div>
      </div>

      <el-divider content-position-left>响应定义(多场景)</el-divider>
      <div v-for="(r, i) in form.responses" :key="i" class="resp-card">
        <div class="resp-head">
          <el-input-number v-model="r.code" :min="100" :max="599" size="small" controls-position="right" style="width:110px" />
          <el-input v-model="r.desc" size="small" placeholder="场景描述,如:成功 / 参数错误" style="width:240px" />
          <el-button size="small" text @click="fmtResp(i)">格式化示例</el-button>
          <div class="grow"></div>
          <el-button size="small" text type="danger" @click="form.responses.splice(i, 1)"><el-icon><Delete /></el-icon></el-button>
        </div>
        <el-input v-model="r.example" type="textarea" :rows="4" class="mono" placeholder="示例返回 JSON" />
      </div>
      <el-button size="small" type="primary" plain @click="addResp"><el-icon><Plus /></el-icon>&nbsp;添加响应场景</el-button>

      <el-divider content-position-left>Mock 配置</el-divider>
      <el-form label-width="120px" class="mock-form">
        <el-form-item label="响应延时(ms)">
          <el-slider v-model="form.mock.delayMs" :min="0" :max="5000" :step="50" show-input style="width:360px" />
        </el-form-item>
        <el-form-item label="强制响应码">
          <el-input-number v-model="form.mock.forceCode" :min="0" :max="599" controls-position="right" style="width:130px" />
          <span class="tip">0=按文档;如 401/500(优先返回同码场景示例)</span>
        </el-form-item>
        <el-form-item label="自定义返回">
          <el-input v-model="form.mock.customJson" type="textarea" :rows="3" class="mono"
            placeholder='脱离 Schema 的自由返回,默认策略下优先' />
        </el-form-item>
        <el-form-item v-if="form.status === 'deprecated'" label="废弃接口 Mock">
          <el-radio-group v-model="form.mock.deprecatedMode">
            <el-radio value="normal">照常 Mock</el-radio>
            <el-radio value="404">返回 404</el-radio>
            <el-radio value="tip">返回废弃提示</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
    </div>

    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="save">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../store'
import { prettyJSON } from '../utils'
import SchemaEditor from './SchemaEditor.vue'
import ParamTable from './ParamTable.vue'

const props = defineProps({
  projectId: { type: Number, required: true },
  project: { type: Object, default: null },
  api: { type: Object, default: null }, // null=新建
  defaultGroupId: { type: Number, default: 0 }
})
const emit = defineEmits(['saved', 'closed'])

const visible = defineModel({ type: Boolean, default: false })
const isEdit = ref(false)
const saving = ref(false)
const genning = ref(false)
const form = ref(null)
const statusList = [
  { value: 'developing', label: '开发中' },
  { value: 'done', label: '已完成' },
  { value: 'deprecated', label: '已废弃' }
]

function blank() {
  return {
    name: '', method: 'GET', path: '', status: 'developing', owner: '', remark: '',
    groupId: props.defaultGroupId || 0,
    pathParams: [], queryParams: [], headerParams: [],
    bodySchema: '', bodyExample: '',
    responses: [{ code: 200, desc: '成功', example: '{\n  \n}' }],
    mock: { delayMs: 0, forceCode: 0, customJson: '', deprecatedMode: 'normal' }
  }
}

function onOpen() {
  isEdit.value = !!props.api
  if (props.api) {
    const a = JSON.parse(JSON.stringify(props.api))
    a.path = a.path.replace(/^\//, '')
    form.value = a
  } else {
    form.value = blank()
  }
}

function addHeader() { form.value.headerParams.push({ key: '', value: '', remark: '', default: true }) }
function addResp() { form.value.responses.push({ code: 400, desc: '', example: '' }) }
function fmtResp(i) {
  form.value.responses[i].example = prettyJSON(form.value.responses[i].example)
}

async function genBodyExample() {
  if (!form.value.bodySchema?.trim()) { ElMessage.info('请先填写 Body Schema'); return }
  genning.value = true
  try {
    const d = await api.post('/gen-example', { schema: form.value.bodySchema })
    if (d.example) form.value.bodyExample = d.example
    else ElMessage.warning('无法从 Schema 生成示例,请检查格式')
  } catch (e) { ElMessage.error(e.message) } finally { genning.value = false }
}

function normalize() {
  const f = JSON.parse(JSON.stringify(form.value))
  f.path = '/' + (f.path || '').trim().replace(/^\/+/, '')
  if (!f.mock) f.mock = { delayMs: 0, forceCode: 0, customJson: '', deprecatedMode: 'normal' }
  return f
}

async function save() {
  const f = normalize()
  if (!f.name.trim()) { ElMessage.warning('请输入接口名称'); return }
  if (f.path === '/') { ElMessage.warning('请输入接口路径'); return }
  saving.value = true
  try {
    if (isEdit.value) await api.patch(`/apis/${props.api.id}`, f, { projectId: props.projectId })
    else await api.post(`/projects/${props.projectId}/apis`, f, { projectId: props.projectId })
    ElMessage.success('已保存')
    visible.value = false
    emit('saved')
  } catch (e) {
    ElMessage.error(e.message)
  } finally { saving.value = false }
}
</script>

<style scoped>
.editor { max-width: 1080px; margin: 0 auto; }
.basic { margin-bottom: 4px; }
.hd-table { padding: 0 12px; }
.hd-row { display: grid; grid-template-columns: 1.2fr 1fr 1fr 110px 40px; gap: 8px; margin-bottom: 6px; align-items: center; }
.hd-head { color: #909399; font-size: 12px; margin-bottom: 2px; }
.global-tip { color: #909399; font-size: 12px; margin-top: 6px; }
.body-sec { display: grid; grid-template-columns: 1.2fr 1fr; gap: 14px; padding: 0 12px; }
.be-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 4px; }
.resp-card { border: 1px solid #e4e7ed; border-radius: 6px; padding: 10px; margin: 0 12px 10px; background: #fafbfc; }
.resp-head { display: flex; gap: 8px; align-items: center; margin-bottom: 6px; }
.grow { flex: 1; }
.mock-form { max-width: 640px; }
.tip { color: #909399; font-size: 12px; margin-left: 10px; }
.mono :deep(textarea), .mono :deep(input) { font-family: Consolas, Monaco, monospace; }
</style>
