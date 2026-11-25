<template>
  <div class="schema-node" :class="{ deep: depth > 0 }">
    <div class="rows">
      <div v-for="(prop, i) in rows" :key="prop.__key" class="row">
        <el-input v-model="prop.name" size="small" placeholder="属性名" style="width: 150px"
          @change="sync(i)" />
        <el-select v-model="prop.type" size="small" style="width: 110px" @change="onTypeChange(i)">
          <el-option v-for="t in allTypes" :key="t" :value="t" :label="t" />
        </el-select>
        <el-checkbox v-model="prop.required" size="small" @change="sync(i)" :disabled="depth > 0">
          <span class="req-label">必填</span>
        </el-checkbox>
        <el-input v-model="prop.desc" size="small" placeholder="说明" style="flex:1" @change="sync(i)" />
        <el-button size="small" text type="danger" @click="remove(i)">
          <el-icon><Delete /></el-icon>
        </el-button>
      </div>
    </div>
    <el-button size="small" text type="primary" @click="add">
      <el-icon><Plus /></el-icon>&nbsp;添加属性
    </el-button>

    <!-- 对象/数组属性的子级 -->
    <template v-for="(prop, i) in rows" :key="'c' + prop.__key">
      <div v-if="prop.type === 'object' || prop.type === 'array'" class="child">
        <div class="child-title">{{ prop.name || '(未命名)' }} 的{{ prop.type === 'array' ? '元素' : '属性' }}</div>
        <!-- 数组元素是单节点;对象是属性列表 -->
        <SchemaNode v-if="prop.type === 'object'" :node="childOf(i, 'properties')" :depth="depth + 1" @changed="childChanged(i, 'properties', $event)" />
        <template v-else>
          <SchemaNode v-if="itemIsObject(i)" :node="childOf(i, 'itemProps')" :depth="depth + 1" @changed="childChanged(i, 'itemProps', $event)" />
          <div v-else class="item-scalar">
            元素类型:
            <el-select :model-value="itemType(i)" size="small" style="width:110px" @change="t => setItemType(i, t)">
              <el-option v-for="t in scalarTypes" :key="t" :value="t" :label="t" />
            </el-select>
          </div>
        </template>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  node: { type: Object, default: () => ({ type: 'object', properties: {} }) },
  depth: { type: Number, default: 0 }
})
const emit = defineEmits(['changed'])

const allTypes = ['string', 'integer', 'number', 'boolean', 'object', 'array']
const scalarTypes = ['string', 'integer', 'number', 'boolean']
let keySeed = 1

const rows = computed(() => {
  const propsMap = props.node?.properties || {}
  const required = props.node?.required || []
  return Object.entries(propsMap).map(([name, sch]) => ({
    __key: 'k' + (keySeed++),
    name,
    type: Array.isArray(sch?.type) ? sch.type[0] : (sch?.type || 'string'),
    required: required.includes(name),
    desc: sch?.description || '',
    __schema: sch || {}
  }))
})

function buildChild(rowsList) {
  const properties = {}, required = []
  for (const r of rowsList) {
    if (!r.name) continue
    const sch = buildSchema(r)
    properties[r.name] = sch
    if (r.required) required.push(r.name)
  }
  return { type: 'object', properties, ...(required.length ? { required } : {}) }
}

function buildSchema(r) {
  const base = { type: r.type }
  if (r.desc) base.description = r.desc
  const sch = r.__schema || {}
  if (r.type === 'object') {
    base.properties = sch.properties || {}
    if (sch.required?.length) base.required = sch.required
  } else if (r.type === 'array') {
    base.items = sch.items || { type: 'string' }
  } else {
    if (sch.enum?.length) base.enum = sch.enum
    if (sch.default !== undefined) base.default = sch.default
    if (sch.example !== undefined) base.example = sch.example
  }
  return base
}

function sync() {
  emit('changed', buildChild(rows.value))
}

function add() {
  const n = buildChild(rows.value)
  n.properties['field' + (Object.keys(n.properties).length + 1)] = { type: 'string' }
  emit('changed', n)
}

function remove(i) {
  const list = rows.value.filter((_, idx) => idx !== i)
  emit('changed', buildChild(list))
}

function onTypeChange(i) {
  const list = rows.value.map((r, idx) => {
    if (idx !== i) return r
    const fresh = { ...r, __schema: { type: r.type, description: r.desc } }
    return fresh
  })
  emit('changed', buildChild(list))
}

// 子级读写
function childOf(i, kind) {
  const r = rows.value[i]
  const sch = r.__schema || {}
  if (kind === 'properties') return { type: 'object', properties: sch.properties || {}, required: sch.required }
  const items = sch.items || { type: 'string' }
  if (items.type === 'object') return { type: 'object', properties: items.properties || {}, required: items.required }
  return { type: 'object', properties: {} }
}
function itemIsObject(i) {
  const sch = rows.value[i].__schema || {}
  return (sch.items?.type) === 'object'
}
function itemType(i) {
  const sch = rows.value[i].__schema || {}
  return sch.items?.type || 'string'
}
function setItemType(i, t) {
  const list = rows.value.map((r, idx) => {
    if (idx !== i) return r
    const sch = { ...(r.__schema || {}) }
    sch.items = t === 'object' ? { type: 'object', properties: {} } : { type: t }
    return { ...r, __schema: sch }
  })
  emit('changed', buildChild(list))
}
function childChanged(i, kind, childSchema) {
  const list = rows.value.map((r, idx) => {
    if (idx !== i) return r
    const sch = { ...(r.__schema || {}) }
    if (kind === 'properties') {
      sch.properties = childSchema.properties
      if (childSchema.required?.length) sch.required = childSchema.required
      else delete sch.required
    } else {
      sch.items = childSchema
    }
    return { ...r, __schema: sch }
  })
  emit('changed', buildChild(list))
}
</script>

<style scoped>
.schema-node { padding: 2px 0; }
.schema-node.deep { border-left: 2px solid #e4e7ed; padding-left: 10px; margin: 6px 0 6px 8px; }
.row { display: flex; gap: 6px; align-items: center; margin-bottom: 6px; }
.req-label { font-size: 12px; }
.child { margin: 4px 0 8px 12px; }
.child-title { font-size: 12px; color: #909399; margin-bottom: 4px; }
.item-scalar { display: flex; align-items: center; gap: 8px; font-size: 13px; color: #606266; padding: 4px 0; }
</style>
