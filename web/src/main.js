import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import 'element-plus/dist/index.css'
import * as Icons from '@element-plus/icons-vue'
import App from './App.vue'
import router, { installGuard } from './router'
import { setPush } from './redirector'

const app = createApp(App)
app.use(createPinia())
installGuard(router) // 路由守卫依赖 pinia,需在其激活后注册
app.use(router)
setPush((path) => router.push(path)) // 注入 SPA 跳转桥
app.use(ElementPlus, { locale: zhCn })
for (const [name, comp] of Object.entries(Icons)) app.component(name, comp)
app.mount('#app')
