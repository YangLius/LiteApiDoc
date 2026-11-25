import { createRouter, createWebHistory } from "vue-router";
import { useAuth } from "./store";

export default createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: "/",
      component: () => import("./views/Projects.vue"),
      meta: { requiresAdmin: true },
    },
    { path: "/login", component: () => import("./views/Login.vue") },
    { path: "/setup", component: () => import("./views/Setup.vue") },
    { path: "/p/:id", component: () => import("./views/ProjectDetail.vue") },
    { path: "/s/:token", component: () => import("./views/Share.vue") },
    {
      path: "/settings",
      component: () => import("./views/GlobalSettings.vue"),
      meta: { requiresAdmin: true },
    },
  ],
});

// 认证决策依赖的初始化状态,每次整页加载(模块重新求值)时强制向服务端核实一次,
// 不信任 localStorage 缓存 —— 服务端重置/换库后缓存会过时,导致已初始化误判
let metaVerified = false;

// 全局守卫:未初始化 → 初始化页;管理页未登录 → 登录页;已登录访问登录/初始化页 → 回首页
export function installGuard(router) {
  router.beforeEach(async (to) => {
    const auth = useAuth();
    if (!metaVerified) {
      metaVerified = true;
      await auth.loadMeta().catch(() => {
        // 仅网络失败时退回缓存(或默认已初始化),避免断网时白屏死循环
        auth.meta = auth.meta || { adminInitialized: true };
      });
    }
    const initialized = !!auth.meta?.adminInitialized;

    if (!initialized && to.path !== "/setup") {
      return {
        path: "/setup",
        query: to.fullPath === "/" ? {} : { redirect: to.fullPath },
      };
    }
    if (initialized && to.path === "/setup") {
      return { path: "/" };
    }
    if (to.meta.requiresAdmin && !auth.isAdmin) {
      return { path: "/login", query: { redirect: to.fullPath } };
    }
    if (to.path === "/login" && auth.isAdmin) {
      return { path: "/" };
    }
    return true;
  });
}
