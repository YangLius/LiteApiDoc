// 极简全局状态:双密码模型的 token 管理(浏览器本地记忆,PRD 第二章)
import { defineStore } from "pinia";
import axios from "axios";
import { gotoLogin } from "./redirector";

// localStorage 可能被手动改坏,损坏时回退 null 而不是让应用崩溒
function safeParse(raw) {
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

export const useAuth = defineStore("auth", {
  state: () => ({
    adminToken: localStorage.getItem("lat_admin_token") || "",
    meta: safeParse(localStorage.getItem("lat_meta")),
  }),
  getters: {
    isAdmin: (s) => !!s.adminToken,
  },
  actions: {
    setAdminToken(t) {
      this.adminToken = t;
      if (t) localStorage.setItem("lat_admin_token", t);
      else localStorage.removeItem("lat_admin_token");
    },
    projectToken(id) {
      return localStorage.getItem(`lat_proj_token_${id}`) || "";
    },
    setProjectToken(id, t) {
      if (t) localStorage.setItem(`lat_proj_token_${id}`, t);
      else localStorage.removeItem(`lat_proj_token_${id}`);
    },
    shareToken(token) {
      return localStorage.getItem(`lat_share_${token}`) || "";
    },
    setShareToken(token, t) {
      if (t) localStorage.setItem(`lat_share_${token}`, t);
      else localStorage.removeItem(`lat_share_${token}`);
    },
    async loadMeta() {
      const { data } = await axios.get("/api/meta");
      this.meta = data.data;
      localStorage.setItem("lat_meta", JSON.stringify(data.data));
      if (this.meta?.title) document.title = this.meta.title;
      return this.meta;
    },
  },
});

// 统一请求封装:{code:0,data,msg} 解包 + 401 触发解锁弹窗事件
export const api = axios.create({ baseURL: "/api", timeout: 35000 });
api.interceptors.request.use((cfg) => {
  const auth = useAuth();
  let token = "";
  if (cfg.adminAuth) token = auth.adminToken;
  else if (cfg.projectId) token = auth.projectToken(cfg.projectId);
  if (token) cfg.headers["X-Auth-Token"] = token;
  return cfg;
});
api.interceptors.response.use(
  (resp) => {
    const body = resp.data;
    if (body && typeof body === "object" && "code" in body) {
      if (body.code === 0) return body.data;
      return Promise.reject(
        Object.assign(new Error(body.msg || "请求失败"), {
          status: body.code,
          body,
        }),
      );
    }
    return body;
  },
  (err) => {
    const body = err.response?.data;
    const cfg = err.config || {};
    const msg = body?.msg || err.message || "网络错误";
    // 管理 token 失效/过期:清除本地状态,经跳转桥回登录页(项目请求不走此分支,
    // 由 needUnlock 触发项目密码解锁弹窗)。回跳目标经 sessionStorage 传递,
    // 消费端(Login.vue)校验站内路径,杜绝开放重定向
    if (err.response?.status === 401 && cfg.adminAuth) {
      useAuth().setAdminToken("");
      if (!location.pathname.startsWith("/login") && !location.pathname.startsWith("/setup")) {
        sessionStorage.setItem("lat_login_next", location.pathname + location.search);
        gotoLogin();
      }
    }
    const e = Object.assign(new Error(msg), {
      status: err.response?.status,
      body,
      needUnlock: err.response?.status === 401 && !cfg.adminAuth,
    });
    return Promise.reject(e);
  },
);
