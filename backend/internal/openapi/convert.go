package openapi

// 规范归一化:把 Swagger 2.0 转换为 OpenAPI 3.0,并对两种规范统一做 $ref 全量内联,
// 使后续 Parse 管线无需感知引用。转换器只保留 LiteApiDoc 文档所需的子集,
// 不识别的字段以警告形式上报,不做静默丢弃。

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxRefDepth = 24 // 自引用/循环 schema 的内联深度上限

// Normalize 将内容归一化为 OpenAPI 3.0 JSON:
//   - OpenAPI 3.0.x:原样保留结构,全量内联 $ref;
//   - Swagger 2.0:转换为 OpenAPI 3.0(host/basePath/schemes→servers、body 参数→requestBody、
//     definitions 引用解析内联),formData 参数与 securityDefinitions 跳过并生成警告。
// 返回规范化 JSON、规范类型("openapi3"/"swagger2")与警告列表。
func Normalize(content string) (out string, kind string, warnings []string, err error) {
	root := map[string]any{}
	if err = yaml.Unmarshal([]byte(content), &root); err != nil {
		return "", "", nil, fmt.Errorf("不是合法的 YAML/JSON 文档: %w", err)
	}
	if root == nil {
		return "", "", nil, fmt.Errorf("文档内容为空")
	}
	// yaml.v3 对数值键映射(如响应码 200:)解码为 map[interface{}]interface{},
	// 必须先统一转成 JSON 友好结构,否则类型断言与遍历都会静默失败
	friendly := toJSONFriendly(root)
	root, _ = friendly.(map[string]any)
	switch {
	case strings.HasPrefix(str(root["openapi"]), "3.0"):
		kind = "openapi3"
	case str(root["swagger"]) == "2.0":
		kind = "swagger2"
		root, warnings = convertSwagger2(root)
	default:
		ver := str(root["openapi"])
		if ver == "" {
			ver = str(root["swagger"])
		}
		return "", "", nil, fmt.Errorf("仅支持 OpenAPI 3.0.x 与 Swagger 2.0,检测到版本 %q", ver)
	}

	seen := map[string]bool{}
	inlineRefs(root, root, 0, seen, &warnings)

	b, err := json.Marshal(root)
	if err != nil {
		return "", "", nil, fmt.Errorf("规范化序列化失败: %w", err)
	}
	return string(b), kind, warnings, nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// toJSONFriendly 递归把 yaml.v3 解析产物转为 JSON 兼容结构:
// map[interface{}]interface{} → map[string]any(数值键转十进制字符串),保证后续
// 类型断言与遍历稳定。map/slice 原地转换以减少拷贝。
func toJSONFriendly(node any) any {
	switch t := node.(type) {
	case map[interface{}]interface{}:
		out := make(map[string]any, len(t))
		for k, v := range t {
			out[scalarKeyString(k)] = toJSONFriendly(v)
		}
		return out
	case map[string]any:
		for k, v := range t {
			t[k] = toJSONFriendly(v)
		}
		return t
	case []any:
		for i := range t {
			t[i] = toJSONFriendly(t[i])
		}
		return t
	default:
		return node
	}
}

func scalarKeyString(k any) string {
	switch s := k.(type) {
	case string:
		return s
	case int:
		return strconv.Itoa(s)
	case int64:
		return strconv.FormatInt(s, 10)
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(s)
	default:
		return fmt.Sprintf("%v", s)
	}
}

// inlineRefs 深度优先遍历,把 map 中的 {"$ref": "#/a/b"} 节点替换为解析后的完整内容。
// 兄弟键(description 等)覆盖在解析结果之上;$ref 解析失败保留原样并记录警告;
// 深度超过 maxRefDepth 视为循环引用,停止内联该分支。
func inlineRefs(node any, root map[string]any, depth int, seen map[string]bool, warnings *[]string) {
	switch t := node.(type) {
	case map[string]any:
		if ref, ok := t["$ref"].(string); ok && strings.HasPrefix(ref, "#/") {
			resolved := resolvePointer(root, ref)
			if resolved == nil {
				key := "missing:" + ref
				if !seen[key] {
					seen[key] = true
					*warnings = append(*warnings, "引用不存在,已原样保留: "+ref)
				}
				return
			}
			if depth >= maxRefDepth {
				key := "deep:" + ref
				if !seen[key] {
					seen[key] = true
					*warnings = append(*warnings, "引用嵌套过深(疑似循环引用),已停止内联: "+ref)
				}
				return
			}
			merged := map[string]any{}
			if rm, ok := resolved.(map[string]any); ok {
				for k, v := range rm {
					merged[k] = v
				}
			} else {
				return // 目标不是对象,无法合并,保持原样
			}
			for k, v := range t {
				if k != "$ref" {
					merged[k] = v
				}
			}
			// 原地合并到 t,保持上层结构不变
			for k := range t {
				delete(t, k)
			}
			for k, v := range merged {
				t[k] = v
			}
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys) // 稳定顺序,避免 map 遍历不确定性导致深度差异
		for _, k := range keys {
			inlineRefs(t[k], root, depth+1, seen, warnings)
		}
	case []any:
		for i := range t {
			inlineRefs(t[i], root, depth+1, seen, warnings)
		}
	}
}

// resolvePointer 按 JSON Pointer(#/a/b/c)从 root 解析节点。
func resolvePointer(root map[string]any, pointer string) any {
	parts := strings.Split(strings.TrimPrefix(pointer, "#/"), "/")
	var cur any = root
	for _, p := range parts {
		p = strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[p]
		if !ok {
			return nil
		}
	}
	return cur
}

// convertSwagger2 Swagger 2.0 → OpenAPI 3.0 核心映射。
func convertSwagger2(root map[string]any) (map[string]any, []string) {
	var warnings []string
	out := map[string]any{
		"openapi": "3.0.3",
		"info":    copyMap(root["info"]),
	}
	if out["info"] == nil {
		out["info"] = map[string]any{"title": "Swagger 导入", "version": "1.0.0"}
	}

	// servers:host + schemes + basePath
	host := str(root["host"])
	basePath := str(root["basePath"])
	schemes, _ := root["schemes"].([]any)
	if len(schemes) == 0 {
		schemes = []any{"http"}
	}
	scheme := str(schemes[0])
	switch {
	case host != "":
		url := scheme + "://" + host
		if basePath != "" {
			url += "/" + strings.Trim(basePath, "/")
		}
		out["servers"] = []any{map[string]any{"url": url}}
	case basePath != "":
		out["servers"] = []any{map[string]any{"url": "/" + strings.Trim(basePath, "/")}}
	}

	if _, ok := root["securityDefinitions"]; ok {
		warnings = append(warnings, "已忽略 securityDefinitions 认证定义(LiteApiDoc 不管理接口鉴权)")
	}
	if _, ok := root["security"]; ok {
		warnings = append(warnings, "已忽略 security 声明")
	}

	paths := map[string]any{}
	srcPaths, _ := root["paths"].(map[string]any)
	formWarned := false
	for p, pv := range srcPaths {
		item, ok := pv.(map[string]any)
		if !ok {
			continue
		}
		newItem := map[string]any{}
		// path 级公共参数
		var pathLevel []any
		if pls, ok := item["parameters"].([]any); ok {
			pathLevel = pls
		}
		if len(pathLevel) > 0 {
			newItem["parameters"] = convertParams(pathLevel, &warnings, &formWarned)
		}
		for method, mv := range item {
			op, ok := mv.(map[string]any)
			if !ok {
				continue // 非 operation 键(x- 扩展、$ref 等)
			}
			newItem[method] = convertOperation(op, &warnings, &formWarned)
		}
		paths[p] = newItem
	}
	out["paths"] = paths

	// definitions 保留在根上供 $ref 内联解析(内联完成后仍保留,体积小、可忽略)
	if defs, ok := root["definitions"]; ok {
		out["definitions"] = defs
	}
	return out, warnings
}

// convertOperation 单个 operation 的参数与响应映射。
func convertOperation(op map[string]any, warnings *[]string, formWarned *bool) map[string]any {
	newOp := map[string]any{}
	for _, k := range []string{"summary", "description", "operationId", "deprecated", "tags"} {
		if v, ok := op[k]; ok {
			newOp[k] = v
		}
	}

	var bodyParam map[string]any
	var params []any
	if ps, ok := op["parameters"].([]any); ok {
		for _, pv := range ps {
			pm, ok := pv.(map[string]any)
			if !ok {
				continue
			}
			switch str(pm["in"]) {
			case "body":
				bodyParam = pm // Swagger2 仅允许一个 body 参数
			case "formData":
				if !*formWarned {
					*formWarned = true
					*warnings = append(*warnings, "formData 表单参数暂不支持(LiteApiDoc V1 仅支持 JSON Body),已跳过")
				}
			default:
				params = append(params, convertParam(pm))
			}
		}
	}
	// path 级参数已在 convertSwagger2 转换并放入 pathItem,operation 级同名覆盖即可
	if len(params) > 0 {
		newOp["parameters"] = params
	}
	if bodyParam != nil {
		schema := bodyParam["schema"]
		consumes := firstConsume(op["consumes"])
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		rb := map[string]any{
			"content": map[string]any{consumes: map[string]any{"schema": schema}},
		}
		if req, ok := bodyParam["required"].(bool); ok && req {
			rb["required"] = true
		}
		newOp["requestBody"] = rb
	}

	// responses
	rs, _ := op["responses"].(map[string]any)
	newRs := map[string]any{}
	for status, rv := range rs {
		rm, ok := rv.(map[string]any)
		if !ok {
			continue
		}
		nr := map[string]any{}
		if d := str(rm["description"]); d != "" {
			nr["description"] = d
		} else {
			nr["description"] = status
		}
		content := map[string]any{}
		if schema, ok := rm["schema"]; ok && schema != nil {
			content["schema"] = schema
		}
		if examples, ok := rm["examples"].(map[string]any); ok {
			if ex, ok := examples["application/json"]; ok {
				content["example"] = ex
			}
		}
		if ex, ok := rm["example"]; ok && content["example"] == nil {
			content["example"] = ex
		}
		if len(content) > 0 {
			nr["content"] = map[string]any{"application/json": content}
		}
		newRs[status] = nr
	}
	if len(newRs) == 0 {
		newRs["200"] = map[string]any{"description": "OK"}
	}
	newOp["responses"] = newRs
	return newOp
}

// convertParams 转换 path 级公共参数。
func convertParams(list []any, warnings *[]string, formWarned *bool) []any {
	var out []any
	for _, pv := range list {
		pm, ok := pv.(map[string]any)
		if !ok {
			continue
		}
		switch str(pm["in"]) {
		case "formData":
			if !*formWarned {
				*formWarned = true
				*warnings = append(*warnings, "formData 表单参数暂不支持(LiteApiDoc V1 仅支持 JSON Body),已跳过")
			}
		default:
			out = append(out, convertParam(pm))
		}
	}
	return out
}

// convertParam Swagger2 参数 → OpenAPI3 参数(参数本体即 schema,剥掉定位字段后作为 schema)。
func convertParam(pm map[string]any) map[string]any {
	schema := map[string]any{}
	for k, v := range pm {
		switch k {
		case "name", "in", "required", "description", "collectionFormat", "allowEmptyValue", "pattern":
			// pattern 属于 schema 关键字,不剥离
			if k != "pattern" {
				continue
			}
			schema[k] = v
		default:
			schema[k] = v
		}
	}
	np := map[string]any{
		"name":   str(pm["name"]),
		"in":     str(pm["in"]),
		"schema": schema,
	}
	if req, ok := pm["required"].(bool); ok && req && str(pm["in"]) != "path" {
		np["required"] = true
	}
	if d := str(pm["description"]); d != "" {
		np["description"] = d
	}
	return np
}

func firstConsume(v any) string {
	list, ok := v.([]any)
	if ok && len(list) > 0 {
		if s := str(list[0]); s != "" {
			return s
		}
	}
	return "application/json"
}

func copyMap(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]any{}
	for k, val := range m {
		out[k] = val
	}
	return out
}
