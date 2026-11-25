<template>
  <el-config-provider>
    <router-view />
  </el-config-provider>
</template>

<style>
body { margin: 0; background: #f5f7fa; font-family: -apple-system, "Segoe UI", "Microsoft YaHei", sans-serif; }
.method-GET { color: #22c55e; }
.method-POST { color: #3b82f6; }
.method-PUT { color: #f59e0b; }
.method-DELETE { color: #ef4444; }
.method-PATCH { color: #a855f7; }
.mono { font-family: Consolas, Monaco, monospace; }
pre.code-block {
  background: #0d1117; color: #e6edf3; padding: 12px; border-radius: 6px;
  overflow: auto; font-size: 12px; margin: 6px 0; white-space: pre-wrap; word-break: break-all;
}
</style>
