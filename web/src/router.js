import { createRouter, createWebHistory } from "vue-router";

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/", component: () => import("./views/Projects.vue") },
    { path: "/p/:id", component: () => import("./views/ProjectDetail.vue") },
    { path: "/s/:token", component: () => import("./views/Share.vue") },
    {
      path: "/settings",
      component: () => import("./views/GlobalSettings.vue"),
    },
  ],
});
