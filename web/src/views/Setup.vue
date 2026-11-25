<template>
  <div class="auth-page">
    <div class="auth-card">
      <div class="brand">
        <img v-if="meta?.logo" :src="meta.logo" class="logo" alt="">
        <h2>{{ meta?.title || 'LiteApiDoc' }}</h2>
        <p class="sub">首次使用,请设置平台管理密码</p>
      </div>

      <el-alert type="info" :closable="false" style="margin-bottom:16px"
        title="管理密码用于:新建/删除项目、平台设置、数据备份。仅设置一次,请妥善保管。" />

      <el-form @submit.prevent="doSetup">
        <el-form-item>
          <el-input v-model="pwd" placeholder="设置密码(至少 4 位)" size="large" show-password />
        </el-form-item>
        <el-form-item>
          <el-input v-model="pwd2" placeholder="确认密码" size="large" show-password @keyup.enter="doSetup" />
        </el-form-item>
        <el-button type="primary" size="large" style="width:100%" :loading="saving" @click="doSetup">
          完成初始化
        </el-button>
      </el-form>

      <p class="foot">v{{ meta?.version }} · 密码以 bcrypt 加盐哈希存储,不保存明文</p>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { api, useAuth } from '../store'

const router = useRouter()
const auth = useAuth()
const meta = computed(() => auth.meta)
const pwd = ref('')
const pwd2 = ref('')
const saving = ref(false)

onMounted(() => auth.loadMeta().catch(() => {}))

async function doSetup() {
  if (pwd.value.length < 4) { ElMessage.warning('密码至少 4 位'); return }
  if (pwd.value !== pwd2.value) { ElMessage.warning('两次输入的密码不一致'); return }
  saving.value = true
  try {
    const d = await api.post('/auth/admin/setup', { password: pwd.value })
    auth.setAdminToken(d.token)
    await auth.loadMeta().catch(() => {})
    ElMessage.success('初始化完成,欢迎使用!')
    router.replace('/')
  } catch (e) {
    if (e.status === 409) {
      ElMessage.warning('管理密码已设置过,请直接登录')
      router.replace('/login')
    } else {
      ElMessage.error(e.message)
    }
  } finally { saving.value = false }
}
</script>

<style scoped>
.auth-page { min-height: 100vh; display: flex; align-items: center; justify-content: center; background: linear-gradient(160deg, #eef2f7 0%, #e4ecf5 100%); }
.auth-card { width: 400px; background: #fff; border-radius: 12px; padding: 36px 32px 24px; box-shadow: 0 8px 30px rgba(0,0,0,.08); }
.brand { text-align: center; margin-bottom: 22px; }
.brand h2 { margin: 8px 0 4px; }
.logo { height: 40px; }
.sub { color: #909399; font-size: 13px; margin: 0; }
.foot { color: #c0c4cc; font-size: 12px; text-align: center; margin-top: 18px; }
</style>
