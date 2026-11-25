// SPA 跳转桥:避免 store.js ↔ router.js 循环依赖。
// main.js 在安装路由后注入 vue-router 的 push;store.js 等非组件模块经此跳转。
let pushFn = null;

export function setPush(fn) {
  pushFn = fn;
}

// 仅用于登录态失效时跳转登录页;目标固定为站内常量路径
export function gotoLogin() {
  if (pushFn) pushFn("/login");
}
