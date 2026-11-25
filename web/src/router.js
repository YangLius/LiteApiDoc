import { createRouter, createWebHistory } from "vue-router";
import { useAuth } from "./store";

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/", component: () => import("./views/Projects.vue"), meta: { requiresAdmin: true } },
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

// 全局守卫:未初始化 → 初始化页;管理页未登录 → 登录页;已登录访问登录/初始化页 → 回首页
export function installGuard(router) {
  router.beforeEach(async (to) => {
    const auth = useAuth();
    // 首次导航时拉取平台元信息;失败降级为"已初始化",由目标页面自行兜错
    if (!auth.meta) {
      await auth.loadMeta().catch(() => {
        auth.meta = auth.meta || { adminInitialized: true };
      });
    }
    const initialized = !!auth.meta?.adminInitialized;

    if (!initialized && to.path !== "/setup") {
      return { path: "/setup", query: to.fullPath === "/" ? {} : { redirect: to.fullPath } };
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
