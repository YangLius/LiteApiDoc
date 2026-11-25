// Package genexample 根据简化 JSON Schema 生成示例数据。
// V1 不做 Faker 模板,仅生成结构正确、类型匹配的简单示例。
package genexample

import (
	"encoding/json"
	"strings"
)

const (
	maxDepth    = 8
	maxProps    = 50
	maxItemsLen = 3
)

// FromSchema 根据给定 Schema 生成示例值(返回任意 JSON 可序列化值)。
func FromSchema(schema json.RawMessage) any {
	if len(schema) == 0 {
		return nil
	}
	var node map[string]any
	if err := json.Unmarshal(schema, &node); err != nil {
		return nil
	}
	return gen(node, 0)
}

// FromSchemaString 便捷封装,返回 JSON 字符串(容错)。
func FromSchemaString(schema string) string {
	if strings.TrimSpace(schema) == "" {
		return ""
	}
	v := FromSchema(json.RawMessage(schema))
	if v == nil {
		return ""
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func gen(node map[string]any, depth int) any {
	if depth > maxDepth {
		return nil
	}
	// 优先使用显式示例/默认值/枚举首项
	if ex, ok := node["example"]; ok && ex != nil {
		return ex
	}
	if def, ok := node["default"]; ok && def != nil {
		return def
	}
	if enum, ok := node["enum"].([]any); ok && len(enum) > 0 {
		return enum[0]
	}

	t := typeName(node)
	switch t {
	case "object":
		out := map[string]any{}
		props, _ := node["properties"].(map[string]any)
		i := 0
		for name, pv := range props {
			if i >= maxProps {
				break
			}
			i++
			p, _ := pv.(map[string]any)
			if p == nil {
				continue
			}
			out[name] = gen(p, depth+1)
		}
		return out
	case "array":
		items, _ := node["items"].(map[string]any)
		if items == nil {
			return []any{}
		}
		return []any{gen(items, depth+1)}
	case "integer":
		return 0
	case "number":
		return 0.0
	case "boolean":
		return false
	case "null":
		return nil
	default: // string 及未声明类型
		return ""
	}
}

func typeName(node map[string]any) string {
	if s, ok := node["type"].(string); ok {
		return s
	}
	// type 为数组(联合类型)时取第一个
	if arr, ok := node["type"].([]any); ok && len(arr) > 0 {
		if s, ok := arr[0].(string); ok {
			return s
		}
	}
	return "string"
}
