<template>
  <div class="param-table">
    <table v-if="rows.length">
      <thead>
        <tr>
          <th style="width:140px">参数名</th>
          <th v-if="kind !== 'header'" style="width:110px">类型</th>
          <th v-if="kind !== 'header'" style="width:60px">必填</th>
          <th>说明</th>
          <th style="width:170px">示例值</th>
          <th style="width:40px"></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(p, i) in rows" :key="i">
          <td><el-input v-model="p.name" size="small" placeholder="name" class="mono" /></td>
          <td v-if="kind !== 'header'">
            <el-select v-model="p.type" size="small">
              <el-option v-for="t in ['string','integer','number','boolean','array']" :key="t" :value="t" />
            </el-select>
          </td>
          <td v-if="kind !== 'header'">
            <el-checkbox v-model="p.required" size="small" :disabled="kind === 'path'" />
          </td>
          <td><el-input v-model="p.desc" size="small" placeholder="参数说明" /></td>
          <td><el-input v-model="p.example" size="small" placeholder="示例" class="mono" /></td>
          <td><el-button size="small" text type="danger" @click="rows.splice(i, 1)"><el-icon><Delete /></el-icon></el-button></td>
        </tr>
      </tbody>
    </table>
    <el-button size="small" text type="primary" @click="addRow">
      <el-icon><Plus /></el-icon>&nbsp;添加参数
    </el-button>
    <div v-if="kind === 'path'" class="tip">路径中的 {x} 占位与此处参数名对应;Path 参数恒为必填</div>
  </div>
</template>

<script setup>
const props = defineProps({
  rows: { type: Array, required: true },
  kind: { type: String, default: 'query' } // path | query
})
function addRow() {
  props.rows.push(
    props.kind === 'header'
      ? { key: '', value: '', remark: '', default: true }
      : { name: '', type: 'string', required: props.kind === 'path', desc: '', example: '' }
  )
}
</script>

<style scoped>
table { width: 100%; border-collapse: collapse; margin-bottom: 4px; }
th { text-align: left; font-size: 12px; color: #909399; padding: 4px 6px; }
td { padding: 3px 6px; }
.tip { color: #909399; font-size: 12px; margin-top: 4px; }
.mono :deep(input) { font-family: Consolas, monospace; }
</style>
