<template>
  <div class="schema-editor">
    <div class="se-tabs">
      <el-radio-group v-model="mode" size="small">
        <el-radio-button value="form">表单模式</el-radio-button>
        <el-radio-button value="source">源码模式</el-radio-button>
      </el-radio-group>
      <el-button size="small" @click="$emit('gen')" :disabled="!hasSchema"><el-icon><MagicStick /></el-icon>&nbsp;生成示例</el-button>
    </div>

    <!-- 源码模式:直接编辑 JSON Schema -->
    <div v-if="mode === 'source'" class="source-wrap">
      <el-input v-model="source" type="textarea" :rows="12" placeholder='{
  "type": "object",
  "properties": { "id": { "type": "integer" } }
}' @blur="applySource" />
      <div v-if="sourceErr" class="src-err">{{ sourceErr }}</div>
    </div>

    <!-- 表单模式:根类型 + 属性树 -->
    <div v-else class="form-wrap">
      <div class="root-row">
        <span class="k">根类型</span>
        <el-select v-model="rootType" size="small" style="width:130px" @change="changeRootType">
          <el-option v-for="t in types" :key="t" :value="t" :label="t" />
        </el-select>
        <span class="tip">对象/数组支持嵌套属性;V1 Body 仅 application/json</span>
      </div>
      <SchemaNode v-if="rootType === 'object' || rootType === 'array'"
        :node="rootSchema" :depth="0" @changed="emitOut" />
      <div v-else class="scalar-tip">根类型为基础类型({{ rootType }}),无需属性配置</div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'

const props = defineProps({ modelValue: { type: String, default: '' } })
const emit = defineEmits(['update:modelValue', 'gen'])

const mode = ref('form')
const source = ref('')
const sourceErr = ref('')
const rootType = ref('object')

const types = ['object', 'array', 'string', 'integer', 'number', 'boolean']

const rootSchema = computed(() => parseSchema(props.modelValue))

function parseSchema(s) {
  try {
    const v = JSON.parse(s || '{}')
    return v && typeof v === 'object' ? v : { type: 'object', properties: {} }
  } catch { return { type: 'object', properties: {} } }
}
const hasSchema = computed(() => !!(props.modelValue && props.modelValue.trim()))

watch(() => props.modelValue, v => {
  if (mode.value === 'form') {
    const sc = parseSchema(v)
    rootType.value = Array.isArray(sc.type) ? sc.type[0] : (sc.type || 'object')
  }
}, { immediate: true })

function emitOut(schema) {
  emit('update:modelValue', JSON.stringify(schema))
}

function changeRootType(t) {
  if (t === 'object') emitOut({ type: 'object', properties: {} })
  else if (t === 'array') emitOut({ type: 'array', items: { type: 'string' } })
  else emitOut({ type: t })
}

function applySource() {
  sourceErr.value = ''
  if (!source.value.trim()) { emit('update:modelValue', ''); return }
  try {
    const v = JSON.parse(source.value)
    emit('update:modelValue', JSON.stringify(v))
  } catch (e) {
    sourceErr.value = 'JSON 解析失败: ' + e.message
  }
}

watch(mode, m => { if (m === 'source') source.value = props.modelValue || '' })
</script>

<style scoped>
.schema-editor { border: 1px solid #e4e7ed; border-radius: 6px; padding: 10px; background: #fafbfc; }
.se-tabs { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.source-wrap .src-err { color: #f56c6c; font-size: 12px; margin-top: 6px; }
.root-row { display: flex; align-items: center; gap: 10px; margin-bottom: 8px; }
.root-row .k { font-size: 13px; color: #606266; width: 56px; }
.tip { color: #909399; font-size: 12px; }
.scalar-tip { color: #909399; font-size: 13px; padding: 10px 4px; }
</style>
