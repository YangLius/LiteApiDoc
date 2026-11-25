<template>
  <aside class="tree">
    <div class="tree-tools">
      <el-input v-model="kw" placeholder="过滤接口名称 / 路径" clearable size="small">
        <template #prefix><el-icon><Search /></el-icon></template>
      </el-input>
      <el-dropdown trigger="click" @command="addGroup">
        <el-button size="small" type="primary" plain><el-icon><FolderAdd /></el-icon>&nbsp;分组</el-button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="0">新建一级分组</el-dropdown-item>
            <el-dropdown-item v-for="g in rootGroups" :key="g.id" :command="String(g.id)" divided>
              在「{{ g.name }}」下建子分组
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
      <el-button size="small" type="primary" @click="$emit('createApi')"><el-icon><Plus /></el-icon>&nbsp;接口</el-button>
    </div>

    <div class="tree-body">
      <div v-if="!tree.length" class="empty">暂无接口,点击上方「接口」创建</div>

      <template v-for="g in tree" :key="'g' + g.id">
        <!-- 一级分组 -->
        <div class="node group"
          draggable="true"
          @dragstart="onDragStart($event, 'group', g.id)"
          @dragover.prevent @drop="onDrop($event, { type: 'group-root', id: g.id })">
          <span class="caret" @click="toggle(g.id)">
            <el-icon><CaretBottom v-if="open[g.id] !== false" /><CaretRight v-else /></el-icon>
          </span>
          <el-icon class="g-icon"><Folder /></el-icon>
          <span class="label">{{ g.name }}</span>
          <span class="ops" @click.stop>
            <el-dropdown trigger="click" size="small" @command="cmd => groupCmd(cmd, g)">
              <el-icon class="more"><MoreFilled /></el-icon>
              <template #dropdown>
                <el-dropdown-menu>
                  <el-dropdown-item :command="'rename'">重命名</el-dropdown-item>
                  <el-dropdown-item :command="'addChild'">新建子分组</el-dropdown-item>
                  <el-dropdown-item :command="'addApi'">在此分组建接口</el-dropdown-item>
                  <el-dropdown-item command="del" divided>删除分组</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
          </span>
        </div>

        <template v-if="open[g.id] !== false">
          <!-- 二级分组 -->
          <template v-for="sub in g.children" :key="'s' + sub.id">
            <div class="node group sub"
              draggable="true"
              @dragstart="onDragStart($event, 'group', sub.id)"
              @dragover.prevent @drop="onDrop($event, { type: 'group-sub', id: g.id, before: sub.id })">
              <el-icon class="g-icon"><FolderOpened /></el-icon>
              <span class="label">{{ sub.name }}</span>
              <span class="ops" @click.stop>
                <el-dropdown trigger="click" size="small" @command="cmd => groupCmd(cmd, sub)">
                  <el-icon class="more"><MoreFilled /></el-icon>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item :command="'rename'">重命名</el-dropdown-item>
                      <el-dropdown-item :command="'addApi'">在此分组建接口</el-dropdown-item>
                      <el-dropdown-item command="del" divided>删除分组</el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
              </span>
            </div>
            <div v-for="a in sub.apis" :key="'a' + a.id" class="node api sub2"
              :class="{ active: a.id === selectedId }"
              draggable="true"
              @dragstart="onDragStart($event, 'api', a.id)"
              @dragover.prevent @drop="onDrop($event, { type: 'api-into', id: sub.id, before: a.id })"
              @click="$emit('select', a.id)">
              <span class="method" :class="a.method">{{ a.method }}</span>
              <span class="label">{{ a.name }}</span>
              <span v-if="a.status === 'deprecated'" class="dep">废</span>
            </div>
          </template>

          <!-- 一级分组直属接口 -->
          <div v-for="a in g.apis" :key="'a' + a.id" class="node api sub"
            :class="{ active: a.id === selectedId }"
            draggable="true"
            @dragstart="onDragStart($event, 'api', a.id)"
            @dragover.prevent @drop="onDrop($event, { type: 'api-into', id: g.id, before: a.id })"
            @click="$emit('select', a.id)">
            <span class="method" :class="a.method">{{ a.method }}</span>
            <span class="label">{{ a.name }}</span>
            <span v-if="a.status === 'deprecated'" class="dep">废</span>
          </div>
        </template>
      </template>

      <!-- 未分组接口 -->
      <template v-if="ungrouped.length">
        <div class="node group" v-if="tree.length">
          <el-icon class="g-icon"><Folder /></el-icon><span class="label">未分组</span>
        </div>
        <div v-for="a in ungrouped" :key="'a' + a.id" class="node api"
          :class="{ active: a.id === selectedId, sub: tree.length > 0 }"
          draggable="true"
          @dragstart="onDragStart($event, 'api', a.id)"
          @dragover.prevent @drop="onDrop($event, { type: 'api-root', before: a.id })"
          @click="$emit('select', a.id)">
          <span class="method" :class="a.method">{{ a.method }}</span>
          <span class="label">{{ a.name }}</span>
          <span v-if="a.status === 'deprecated'" class="dep">废</span>
        </div>
      </template>
    </div>
  </aside>
</template>

<script setup>
import { computed, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'

const props = defineProps({
  groups: { type: Array, default: () => [] },
  apis: { type: Array, default: () => [] },
  selectedId: { type: Number, default: 0 }
})
const emit = defineEmits(['select', 'createApi', 'createGroup', 'renameGroup', 'delGroup', 'sortGroups', 'moveApi', 'changed'])

const kw = ref('')
const open = ref({})
const drag = ref(null)

const rootGroups = computed(() => props.groups.filter(g => !g.parentId))

// 关键字过滤 + 组树组装
const tree = computed(() => {
  const k = kw.value.trim().toLowerCase()
  const match = a => !k || a.name.toLowerCase().includes(k) || a.path.toLowerCase().includes(k)
  return rootGroups.value.map(g => {
    const children = props.groups.filter(s => s.parentId === g.id)
      .map(s => ({ ...s, apis: props.apis.filter(a => a.groupId === s.id && match(a)) }))
      .filter(s => !k || s.apis.length)
    const apis = props.apis.filter(a => a.groupId === g.id && match(a))
    return { ...g, children, apis }
  }).filter(g => !k || g.apis.length || g.children.length)
})
const ungrouped = computed(() => props.apis.filter(a => !a.groupId && (!kw.value ||
  a.name.toLowerCase().includes(kw.value.trim().toLowerCase()) ||
  a.path.toLowerCase().includes(kw.value.trim().toLowerCase()))))

function toggle(id) { open.value[id] = open.value[id] === false ? true : false }

function onDragStart(ev, type, id) { drag.value = { type, id: Number(id) } }

async function onDrop(ev, target) {
  const d = drag.value
  drag.value = null
  if (!d) return
  try {
    if (d.type === 'api') {
      // 接口拖拽:落点分组与排序
      let groupId = 0, sortAfter = null
      if (target.type === 'api-into' || target.type === 'api-root') {
        groupId = target.type === 'api-into' ? target.id : 0
        sortAfter = target.before
      } else if (target.type === 'group-root' || target.type === 'group-sub') {
        groupId = target.id
      }
      await emitMoveApi(d.id, groupId, sortAfter)
    } else if (d.type === 'group') {
      // 分组拖拽:纯排序走 sort;跨级移动走 move
      if (target.type === 'group-root' || target.type === 'group-sub') {
        const targetGroup = target.type === 'group-root'
          ? props.groups.find(g => g.id === target.id)
          : props.groups.find(g => g.id === target.before)
        const parentId = targetGroup?.parentId || 0
        await emitSortGroups(d.id, parentId, target.before)
      }
    }
  } catch (e) { ElMessage.error(e.message) }
}

async function emitMoveApi(apiId, groupId, beforeApiId) {
  const moving = props.apis.find(a => a.id === apiId)
  if (!moving) return
  const siblings = props.apis.filter(a => a.groupId === groupId && a.id !== apiId)
  const items = []
  if (beforeApiId) {
    for (const a of siblings) {
      if (a.id === beforeApiId) { items.push({ id: apiId, groupId, sortOrder: a.sortOrder }); items.push({ id: a.id, groupId, sortOrder: a.sortOrder + 1 }) }
      else items.push({ id: a.id, groupId, sortOrder: a.sortOrder })
    }
  } else {
    const maxSort = siblings.reduce((m, a) => Math.max(m, a.sortOrder), 0)
    items.push({ id: apiId, groupId, sortOrder: maxSort + 1 })
    siblings.forEach(a => items.push({ id: a.id, groupId, sortOrder: a.sortOrder }))
  }
  emit('moveApi', items)
}

async function emitSortGroups(dragId, parentId, beforeId) {
  // 简化:移动到 parentId 下的末尾;若指定 before 则插到其前
  const inTarget = props.groups.filter(g => g.parentId === parentId && g.id !== dragId)
  const items = []
  let sort = 0
  for (const g of inTarget) {
    if (g.id === beforeId) items.push({ id: dragId, parentId, sortOrder: sort++ })
    items.push({ id: g.id, parentId: g.parentId, sortOrder: sort++ })
  }
  if (!items.find(i => i.id === dragId)) items.push({ id: dragId, parentId, sortOrder: sort++ })
  // 其余不动
  for (const g of props.groups) {
    if (!items.find(i => i.id === g.id)) items.push({ id: g.id, parentId: g.parentId, sortOrder: g.sortOrder })
  }
  emit('sortGroups', items)
}

async function addGroup(parentId) {
  try {
    const { value } = await ElMessageBox.prompt('分组名称', '新建分组', { inputValue: '', inputPattern: /\S+/, inputErrorMessage: '名称不能为空' })
    emit('createGroup', { name: value.trim(), parentId: Number(parentId) })
  } catch {}
}

async function groupCmd(cmd, g) {
  try {
    if (cmd === 'rename') {
      const { value } = await ElMessageBox.prompt('分组名称', '重命名', { inputValue: g.name, inputPattern: /\S+/, inputErrorMessage: '名称不能为空' })
      emit('renameGroup', { id: g.id, name: value.trim() })
    } else if (cmd === 'addChild') {
      const { value } = await ElMessageBox.prompt('子分组名称', '新建子分组', { inputPattern: /\S+/, inputErrorMessage: '名称不能为空' })
      emit('createGroup', { name: value.trim(), parentId: g.id })
    } else if (cmd === 'addApi') {
      emit('createApi', g.id)
    } else if (cmd === 'del') {
      await ElMessageBox.confirm(`删除「${g.name}」:子分组一并删除,组内接口移至未分组。确认?`, '删除分组', { type: 'warning' })
      emit('delGroup', g.id)
    }
  } catch {}
}
</script>

<style scoped>
.tree { width: 280px; background: #fff; border-right: 1px solid #e4e7ed; display: flex; flex-direction: column; height: calc(100vh - 56px); }
.tree-tools { display: flex; gap: 6px; padding: 10px; border-bottom: 1px solid #f0f0f0; align-items: center; }
.tree-tools .el-input { flex: 1; }
.tree-body { flex: 1; overflow: auto; padding: 8px 6px; }
.empty { color: #c0c4cc; font-size: 13px; text-align: center; padding: 30px 0; }
.node { display: flex; align-items: center; gap: 6px; padding: 6px 8px; border-radius: 6px; cursor: pointer; font-size: 13px; user-select: none; }
.node:hover { background: #f5f7fa; }
.node.active { background: #ecf5ff; color: #409eff; }
.node.sub { padding-left: 22px; }
.node.sub2 { padding-left: 36px; }
.node .caret { display: flex; color: #909399; }
.g-icon { color: #e6a23c; }
.label { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ops { visibility: hidden; }
.node:hover .ops { visibility: visible; }
.more { color: #909399; }
.method { font-size: 11px; font-weight: 700; width: 46px; flex-shrink: 0; }
.dep { color: #e6a23c; font-size: 11px; border: 1px solid #e6a23c; border-radius: 3px; padding: 0 3px; }
</style>
