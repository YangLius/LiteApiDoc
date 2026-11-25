<template>
  <div class="auth-page">
    <div class="auth-card">
      <div class="brand">
        <img v-if="meta?.logo" :src="meta.logo" class="logo" alt="">
        <h2>{{ meta?.title || 'LiteApiDoc' }}</h2>
        <p class="sub">接口文档 + Mock 一体化平台</p>
      </div>

      <el-form @submit.prevent="doLogin">
        <el-form-item>
          <el-input v-model="pwd" placeholder="平台管理密码" size="large" show-password
            @keyup.enter="doLogin" autofocus />
        </el-form-item>
        <el-button type="primary" size="large" style="width:100%" :loading="saving" @click="doLogin">
          登 录
        </el-button>
      </el-form>

      <p class="foot">v{{ meta?.version }} · 密码连续错误 5 次将锁定 10 分钟</p>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { api, useAuth } from '../store'

const route = useRoute()
const router = useRouter()
const auth = useAuth()
const meta = computed(() => auth.meta)
const pwd = ref('')
const saving = ref(false)

onMounted(() => auth.loadMeta().catch(() => {}))

// 回跳目标仅允许站内路径,防开放重定向(// 外部协议相对路径、/\ 同样拒绝)。
// 优先取 sessionStorage(token 过期踢回时写入,同源才可写,不可被外部篡改),
// 其次取 ?redirect= 查询参数(登录页直链场景,必须校验)。
function takeTarget() {
  const stored = sessionStorage.getItem("lat_login_next") || "";
  sessionStorage.removeItem("lat_login_next");
  const s = String(stored || route.query.redirect || "");
  return s.startsWith("/") && !s.startsWith("//") && !s.startsWith("/\\") ? s : "/";
}

async function doLogin() {
  if (!pwd.value) { ElMessage.warning('请输入平台管理密码'); return }
  saving.value = true
  try {
    const d = await api.post('/auth/admin/login', { password: pwd.value })
    auth.setAdminToken(d.token)
    await auth.loadMeta().catch(() => {})
    ElMessage.success('登录成功')
    router.replace(takeTarget())
  } catch (e) {
    ElMessage.error(e.message)
  } finally { saving.value = false }
}
</script>

<style scoped>
.auth-page { min-height: 100vh; display: flex; align-items: center; justify-content: center; background: linear-gradient(160deg, #eef2f7 0%, #e4ecf5 100%); }
.auth-card { width: 380px; background: #fff; border-radius: 12px; padding: 36px 32px 24px; box-shadow: 0 8px 30px rgba(0,0,0,.08); }
.brand { text-align: center; margin-bottom: 26px; }
.brand h2 { margin: 8px 0 4px; }
.logo { height: 40px; }
.sub { color: #909399; font-size: 13px; margin: 0; }
.foot { color: #c0c4cc; font-size: 12px; text-align: center; margin-top: 18px; }
</style>
