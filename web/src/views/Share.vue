<template>
  <div class="page">
    <header class="topbar">
      <span class="brand">{{ doc?.project?.name || '…' }}</span>
      <span class="sub">只读分享 · LiteApiDoc</span>
      <div class="grow"></div>
      <span v-if="doc" class="mono mock-base">Mock 基础地址:{{ mockBase }}</span>
    </header>

    <main class="main" v-loading="loading">
      <!-- 锁定:输入分享密码 -->
      <div v-if="locked" class="lock-box">
        <el-result icon="warning" title="该分享已设置访问密码">
          <template #extra>
            <div class="lock-form">
              <el-input v-model="pwd" placeholder="分享密码" show-password @keyup.enter="unlock" style="width:240px" />
              <el-button type="primary" :loading="unlocking" @click="unlock">解锁查看</el-button>
            </div>
          </template>
        </el-result>
      </div>

      <template v-else-if="doc">
        <p v-if="doc.project.description" class="desc">{{ doc.project.description }}</p>
        <el-empty v-if="!allApis.length" description="该项目暂无接口" />
        <template v-for="g in tree" :key="'g' + g.id">
          <div class="group">
            <h3><el-icon><Folder /></el-icon>&nbsp;{{ g.name }}</h3>
            <template v-for="sub in g.children" :key="'s' + sub.id">
              <h4 class="sub">{{ sub.name }}</h4>
              <ApiBlock v-for="a in sub.apis" :key="a.id" :api="a" :mock-base="mockBase" />
            </template>
            <ApiBlock v-for="a in g.apis" :key="a.id" :api="a" :mock-base="mockBase" />
          </div>
        </template>
        <div class="group" v-if="ungrouped.length">
          <h3><el-icon><Folder /></el-icon>&nbsp;未分组</h3>
          <ApiBlock v-for="a in ungrouped" :key="a.id" :api="a" :mock-base="mockBase" />
        </div>
      </template>
    </main>
  </div>
</template>

<script setup>
import { computed, defineComponent, h, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import axios from 'axios'
import { useAuth } from '../store'
import { prettyJSON } from '../utils'

const route = useRoute()
const auth = useAuth()
const token = route.params.token

const loading = ref(true)
const locked = ref(false)
const unlocking = ref(false)
const pwd = ref('')
const doc = ref(null)

const mockBase = computed(() => {
  const host = location.hostname || 'localhost'
  const port = auth.meta?.mockPort || 9901
  return `http://${host}:${port}/mock/${doc.value?.project?.id || ''}`
})

const tree = computed(() => {
  if (!doc.value) return []
  const { groups, apis } = doc.value
  return groups.filter(g => !g.parentId).map(g => ({
    ...g,
    children: groups.filter(s => s.parentId === g.id)
      .map(s => ({ ...s, apis: apis.filter(a => a.groupId === s.id) })),
    apis: apis.filter(a => a.groupId === g.id)
  }))
})
const ungrouped = computed(() => doc.value ? doc.value.apis.filter(a => !a.groupId) : [])
const allApis = computed(() => doc.value ? doc.value.apis : [])

// axios 直连(分享接口自定义 401 语义)
async function raw(url, tokenHdr) {
  const headers = tokenHdr ? { 'X-Auth-Token': tokenHdr } : {}
  const resp = await axios.get(url, { headers })
  return resp.data?.data
}

async function load() {
  loading.value = true
  try {
    const info = await raw(`/api/share/${token}`)
    if (info.locked) {
      locked.value = true
    } else {
      locked.value = false
      doc.value = await raw(`/api/share/${token}/doc`, auth.shareToken(token))
    }
  } catch (e) {
    if (e.response?.status === 401) locked.value = true
    else doc.value = null
  } finally { loading.value = false }
}
onMounted(async () => { await auth.loadMeta().catch(() => {}); load() })

async function unlock() {
  unlocking.value = true
  try {
    const resp = await axios.post(`/api/share/${token}/unlock`, { password: pwd.value })
    const t = resp.data?.data?.token || ''
    auth.setShareToken(token, t)
    locked.value = false
    doc.value = await raw(`/api/share/${token}/doc`, t)
  } catch (e) {
    alert(e.response?.data?.msg || '密码错误')
  } finally { unlocking.value = false }
}

// 简洁只读接口块
const ApiBlock = defineComponent({
  props: { api: Object, mockBase: String },
  setup(p) {
    const methodColor = { GET: '#22c55e', POST: '#3b82f6', PUT: '#f59e0b', DELETE: '#ef4444', PATCH: '#a855f7' }
    const tbl = (title, rows, head) => rows?.length ? h('div', { class: 'sec' }, [
      h('b', null, title),
      h('table', null, [
        h('thead', null, h('tr', null, head.map(x => h('th', null, x)))),
        h('tbody', null, rows.map(r => h('tr', null, r.map(c => h('td', { class: c.mono ? 'mono' : '' }, c.t)))))
      ])
    ]) : null
    return () => h('div', { class: 'api' }, [
      h('div', { class: 'api-head' }, [
        h('span', { class: 'method mono', style: { color: methodColor[p.api.method] || '#666' } }, p.api.method),
        h('span', { class: 'path mono' }, p.api.path),
        p.api.status === 'deprecated' ? h('span', { class: 'dep' }, '已废弃') : null,
        h('span', { class: 'api-name' }, p.api.name)
      ]),
      p.api.remark ? h('p', { class: 'remark' }, p.api.remark) : null,
      tbl('Path 参数', (p.api.pathParams || []).map(x => [{ t: x.name, mono: true }, { t: x.type }, { t: '必填' }, { t: x.desc }, { t: x.example, mono: true }]), ['名称', '类型', '必填', '说明', '示例']),
      tbl('Query 参数', (p.api.queryParams || []).map(x => [{ t: x.name, mono: true }, { t: x.type }, { t: x.required ? '必填' : '可选' }, { t: x.desc }, { t: x.example, mono: true }]), ['名称', '类型', '必填', '说明', '示例']),
      tbl('Header', (p.api.headerParams || []).map(x => [{ t: x.key, mono: true }, { t: x.value, mono: true }, { t: x.remark }]), ['Key', '示例值', '备注']),
      (p.api.bodyExample ? h('div', { class: 'sec' }, [h('b', null, 'Body 示例'), h('pre', { class: 'code-block' }, prettyJSON(p.api.bodyExample))]) : null),
      ...(p.api.responses || []).map(r => h('div', { class: 'sec' }, [
        h('b', null, `${r.code} ${r.desc || ''}`),
        r.example ? h('pre', { class: 'code-block' }, prettyJSON(r.example)) : null
      ])),
      h('div', { class: 'sec mock' }, [
        h('b', null, 'Mock'),
        h('span', { class: 'mono mock-url' }, `${p.mockBase}${p.api.path}`)
      ])
    ])
  }
})
</script>

<style scoped>
.page { min-height: 100vh; background: #f5f7fa; }
.topbar { display: flex; align-items: center; gap: 12px; padding: 0 24px; height: 56px; background: #fff; border-bottom: 1px solid #e4e7ed; position: sticky; top: 0; z-index: 5; }
.brand { font-size: 18px; font-weight: 700; }
.sub { color: #909399; font-size: 13px; }
.grow { flex: 1; }
.mock-base { font-size: 12px; color: #67c23a; }
.main { max-width: 960px; margin: 0 auto; padding: 20px 16px; }
.desc { color: #57606a; }
.group { background: #fff; border: 1px solid #e4e7ed; border-radius: 8px; padding: 14px 18px; margin-bottom: 16px; }
.group h3 { display: flex; align-items: center; margin: 4px 0 12px; font-size: 16px; }
.group h4.sub { margin: 14px 0 6px; color: #606266; border-bottom: 1px dashed #e4e7ed; padding-bottom: 4px; }
.api { border-top: 1px solid #f0f0f0; padding: 12px 2px; }
.api:first-of-type { border-top: none; }
.api-head { display: flex; align-items: center; gap: 10px; }
.method { font-weight: 800; }
.path { background: #eff2f5; padding: 2px 8px; border-radius: 4px; font-size: 13px; }
.api-name { color: #606266; font-size: 14px; }
.dep { color: #e6a23c; font-size: 12px; border: 1px solid #e6a23c; border-radius: 3px; padding: 0 4px; }
.remark { color: #909399; font-size: 13px; }
.sec { margin-top: 8px; font-size: 13px; }
.sec b { color: #606266; margin-right: 8px; }
.sec table { border-collapse: collapse; margin-top: 6px; width: 100%; }
.sec th, .sec td { border: 1px solid #e4e7ed; padding: 4px 9px; text-align: left; font-size: 12px; }
.sec th { background: #f5f7fa; color: #606266; }
.mock-url { background: #f0f9eb; border: 1px solid #e1f3d8; padding: 2px 8px; border-radius: 4px; font-size: 12px; }
.mono { font-family: Consolas, monospace; }
.lock-box { background: #fff; border: 1px solid #e4e7ed; border-radius: 8px; margin-top: 40px; }
.lock-form { display: flex; gap: 10px; justify-content: center; }
</style>
