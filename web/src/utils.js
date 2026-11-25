// 通用工具:复制、时间、curl、Mock 地址
export function fmtTime(ts) {
  if (!ts) return "-";
  const d = new Date(ts * 1000);
  const p = (n) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

export async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    const ta = document.createElement("textarea");
    ta.value = text;
    document.body.appendChild(ta);
    ta.select();
    document.execCommand("copy");
    document.body.removeChild(ta);
    return true;
  }
}

// 依据接口定义生成 curl 命令(文档页复制用)
export function buildCurl(api, base) {
  const b = base || "";
  let url = `${b}${api.path}`;
  const qs = (api.queryParams || [])
    .filter((p) => p.name)
    .map((p) => `${p.name}=${encodeURIComponent(p.example || "")}`);
  if (qs.length) url += "?" + qs.join("&");
  const lines = [`curl -X ${api.method} "${url}"`];
  for (const h of api.headerParams || []) {
    if (h.key) lines.push(`  -H "${h.key}: ${h.value || ""}"`);
  }
  if (api.bodyExample && api.method !== "GET" && api.method !== "DELETE") {
    lines.push(`  -H "Content-Type: application/json"`);
    lines.push(`  -d '${api.bodyExample.replace(/'/g, "'\\''")}'`);
  }
  return lines.join(" \\\n");
}

// Mock 完整地址
export function mockUrl(meta, projectId, path) {
  const host = location.hostname || "localhost";
  return `http://${host}:${meta?.mockPort || 9901}/mock/${projectId}${path || ""}`;
}

// 简化 JSON Schema → 示例(前端即时预览;复杂场景走后端 /api/gen-example)
export function genExampleLocal(schema) {
  if (!schema || typeof schema !== "object") return "";
  function gen(node, depth) {
    if (depth > 8) return null;
    if (node.example !== undefined) return node.example;
    if (node.default !== undefined) return node.default;
    if (Array.isArray(node.enum) && node.enum.length) return node.enum[0];
    const t = Array.isArray(node.type) ? node.type[0] : node.type || "string";
    if (t === "object") {
      const out = {};
      const props = node.properties || {};
      for (const [k, v] of Object.entries(props)) out[k] = gen(v, depth + 1);
      return out;
    }
    if (t === "array") return node.items ? [gen(node.items, depth + 1)] : [];
    if (t === "integer") return 0;
    if (t === "number") return 0;
    if (t === "boolean") return false;
    if (t === "null") return null;
    return "";
  }
  return JSON.stringify(gen(schema, 0), null, 2);
}

export function prettyJSON(s) {
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s || "";
  }
}
