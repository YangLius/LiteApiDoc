// Package openapi 实现 OpenAPI 3.x 文档的导入解析与导出构建。
// 支持 3.0.x / 3.1.x(FastAPI 默认产出 3.1);yaml/json 通吃(yaml 是 json 超集)。
package openapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"liteapidoc/internal/model"
)

// ParsedApi 解析得到的接口条目。
type ParsedApi struct {
	Name         string
	Method       string
	Path         string
	Remark       string
	PathParams   []model.Param
	QueryParams  []model.Param
	HeaderParams []model.HeaderParam
	BodySchema   string
	Responses    []model.ResponseDef
}

var supportedMethods = map[string]bool{
	"get": true, "post": true, "put": true, "delete": true, "patch": true,
}

// Parse 解析 OpenAPI3.0 内容,返回 basePath(servers[0] 路径前缀)与接口列表。
func Parse(content string) (basePath string, apis []ParsedApi, title string, err error) {
	root := map[string]any{}
	if err = yaml.Unmarshal([]byte(content), &root); err != nil {
		return "", nil, "", fmt.Errorf("不是合法的 YAML/JSON 文档: %w", err)
	}
	if root == nil {
		return "", nil, "", fmt.Errorf("文档内容为空")
	}
	ver, _ := root["openapi"].(string)
	if !strings.HasPrefix(ver, "3.") {
		return "", nil, "", fmt.Errorf("仅支持 OpenAPI 3.x(3.0/3.1),当前版本为 %q", ver)
	}

	if info, ok := root["info"].(map[string]any); ok {
		title, _ = info["title"].(string)
	}
	basePath = extractBasePath(root["servers"])

	paths, _ := root["paths"].(map[string]any)
	for p, pv := range paths {
		pathItem, ok := pv.(map[string]any)
		if !ok {
			continue
		}
		// path 级公共 parameters
		var pathLevelParams []map[string]any
		if pls, ok := pathItem["parameters"].([]any); ok {
			for _, pp := range pls {
				if m, ok := pp.(map[string]any); ok {
					pathLevelParams = append(pathLevelParams, m)
				}
			}
		}
		for method, ov := range pathItem {
			if !supportedMethods[strings.ToLower(method)] {
				continue
			}
			op, ok := ov.(map[string]any)
			if !ok {
				continue
			}
			apis = append(apis, buildParsedApi(p, method, op, pathLevelParams))
		}
	}
	sortApis(apis)
	return basePath, apis, title, nil
}

func sortApis(apis []ParsedApi) {
	sort.Slice(apis, func(i, j int) bool {
		if apis[i].Path != apis[j].Path {
			return apis[i].Path < apis[j].Path
		}
		return apis[i].Method < apis[j].Method
	})
}

// hasCJK 判断字符串是否含中日韩字符。
func hasCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// nameFromDesc 取描述第一行作为接口名,超长截断。
func nameFromDesc(desc string) string {
	if i := strings.IndexByte(desc, '\n'); i >= 0 {
		desc = desc[:i]
	}
	desc = strings.TrimSpace(desc)
	if r := []rune(desc); len(r) > 60 {
		desc = string(r[:60]) + "…"
	}
	return desc
}

// pickName 接口命名优先级:summary 与 description 都可能有中英文。
// FastAPI 等框架在未显式指定 summary 时会从函数名自动生成英文 summary,
// 而真正的中文描述在 description(docstring)里 —— 此时优先用中文 description;
// 否则用 summary;都没有时回退 method+path。
func pickName(summary, desc, method, path string) string {
	summary = strings.TrimSpace(summary)
	desc = strings.TrimSpace(desc)
	if summary != "" && (hasCJK(summary) || !hasCJK(desc)) {
		return summary
	}
	if desc != "" && hasCJK(desc) {
		if n := nameFromDesc(desc); n != "" {
			return n
		}
	}
	if summary != "" {
		return summary
	}
	return method + " " + path
}

func buildParsedApi(path, method string, op map[string]any, pathLevelParams []map[string]any) ParsedApi {
	a := ParsedApi{Method: strings.ToUpper(method), Path: normalizePath(path)}
	summary, _ := op["summary"].(string)
	desc, _ := op["description"].(string)
	a.Name = pickName(summary, desc, method, path)
	if d := strings.TrimSpace(desc); d != "" {
		a.Remark = d
	}

	// parameters:path 级 + operation 级(operation 同名覆盖)
	merged := map[string]map[string]any{} // key: in:name
	for _, p := range pathLevelParams {
		key := paramKey(p)
		merged[key] = p
	}
	if ps, ok := op["parameters"].([]any); ok {
		for _, pv := range ps {
			if m, ok := pv.(map[string]any); ok {
				merged[paramKey(m)] = m
			}
		}
	}
	// 稳定顺序输出
	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := merged[k]
		in, _ := p["in"].(string)
		name, _ := p["name"].(string)
		required, _ := p["required"].(bool)
		desc, _ := p["description"].(string)
		schema, _ := p["schema"].(map[string]any)
		example := ""
		if e, ok := p["example"]; ok {
			example = stringifyScalar(e)
		} else if schema != nil {
			if e, ok := schema["default"]; ok {
				example = stringifyScalar(e)
			}
		}
		switch in {
		case "path":
			a.PathParams = append(a.PathParams, model.Param{
				Name: name, Type: schemaType(schema), Required: true, Desc: desc, Example: example,
			})
		case "query":
			a.QueryParams = append(a.QueryParams, model.Param{
				Name: name, Type: schemaType(schema), Required: required, Desc: desc, Example: example,
			})
		case "header":
			a.HeaderParams = append(a.HeaderParams, model.HeaderParam{Key: name, Value: example, Remark: desc})
		}
	}

	// requestBody → BodySchema
	if rb, ok := op["requestBody"].(map[string]any); ok {
		if content, ok := rb["content"].(map[string]any); ok {
			if jc, ok := content["application/json"].(map[string]any); ok {
				if schema, ok := jc["schema"]; ok && schema != nil {
					if b, err := json.Marshal(schema); err == nil {
						a.BodySchema = string(b)
					}
				}
			}
		}
	}

	// responses
	if rs, ok := op["responses"].(map[string]any); ok {
		codes := make([]string, 0, len(rs))
		for c := range rs {
			codes = append(codes, c)
		}
		sort.Strings(codes)
		for _, c := range codes {
			code := parseCode(c)
			if code <= 0 {
				continue
			}
			r, _ := rs[c].(map[string]any)
			def := model.ResponseDef{Code: code}
			if r != nil {
				if d, ok := r["description"].(string); ok {
					def.Desc = d
				}
				if content, ok := r["content"].(map[string]any); ok {
					if jc, ok := content["application/json"].(map[string]any); ok {
						if schema, ok := jc["schema"]; ok && schema != nil {
							if b, err := json.Marshal(schema); err == nil {
								def.Schema = json.RawMessage(b)
							}
						}
						if ex, ok := jc["example"]; ok {
							def.Example = marshalPretty(ex)
						} else if examples, ok := jc["examples"].(map[string]any); ok && len(examples) > 0 {
							// 取第一个 example 的 value
							names := make([]string, 0, len(examples))
							for n := range examples {
								names = append(names, n)
							}
							sort.Strings(names)
							if exObj, ok := examples[names[0]].(map[string]any); ok {
								def.Example = marshalPretty(exObj["value"])
							}
						}
					}
				}
			}
			a.Responses = append(a.Responses, def)
		}
	}
	return a
}

func paramKey(p map[string]any) string {
	in, _ := p["in"].(string)
	name, _ := p["name"].(string)
	return in + ":" + name
}

func parseCode(c string) int {
	var n int
	if _, err := fmt.Sscanf(c, "%d", &n); err == nil {
		return n
	}
	return 0
}

func schemaType(schema map[string]any) string {
	if schema == nil {
		return "string"
	}
	if t, ok := schema["type"].(string); ok {
		return t
	}
	return "string"
}

func stringifyScalar(v any) string {
	switch s := v.(type) {
	case string:
		return s
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func marshalPretty(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func normalizePath(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// extractBasePath 从 servers[0].url 提取路径前缀(如 https://api.x.com/v1 → /v1)。
func extractBasePath(servers any) string {
	list, ok := servers.([]any)
	if !ok || len(list) == 0 {
		return ""
	}
	s0, ok := list[0].(map[string]any)
	if !ok {
		return ""
	}
	u, _ := s0["url"].(string)
	idx := strings.Index(u, "://")
	if idx >= 0 {
		rest := u[idx+3:]
		if slash := strings.Index(rest, "/"); slash >= 0 {
			u = rest[slash:]
		} else {
			u = ""
		}
	}
	u = strings.TrimSuffix(u, "/")
	if u != "" && !strings.HasPrefix(u, "/") {
		return ""
	}
	return u
}

// Build 构建导出用 OpenAPI3.0 map。commonPrefix 写回 servers,并从 path 中剔除。
func Build(apis []model.Api, groups []model.Group, title, description, commonPrefix string) map[string]any {
	groupName := map[int64]string{}
	for _, g := range groups {
		groupName[g.ID] = g.Name
	}

	paths := map[string]any{}
	for _, a := range apis {
		p := a.Path
		if commonPrefix != "" && strings.HasPrefix(p, commonPrefix) {
			p = strings.TrimPrefix(p, commonPrefix)
			if p == "" {
				p = "/"
			}
		}
		pathItem, _ := paths[p].(map[string]any)
		if pathItem == nil {
			pathItem = map[string]any{}
			paths[p] = pathItem
		}
		op := map[string]any{
			"summary": a.Name,
		}
		if a.Remark != "" {
			op["description"] = a.Remark
		}
		if gname := groupName[a.GroupID]; gname != "" {
			op["tags"] = []string{gname}
		}
		params := []any{}
		for _, pp := range a.PathParams {
			params = append(params, paramMap("path", pp.Name, pp.Type, true, pp.Desc, pp.Example))
		}
		for _, qp := range a.QueryParams {
			params = append(params, paramMap("query", qp.Name, qp.Type, qp.Required, qp.Desc, qp.Example))
		}
		for _, hp := range a.HeaderParams {
			m := map[string]any{"in": "header", "name": hp.Key, "schema": map[string]any{"type": "string"}}
			if hp.Remark != "" {
				m["description"] = hp.Remark
			}
			params = append(params, m)
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		if len(a.BodySchema) > 0 && json.Valid([]byte(a.BodySchema)) {
			var schema any
			if err := json.Unmarshal([]byte(a.BodySchema), &schema); err == nil {
				op["requestBody"] = map[string]any{
					"required": false,
					"content":  map[string]any{"application/json": map[string]any{"schema": schema}},
				}
			}
		}
		resps := map[string]any{}
		for _, r := range a.Responses {
			if r.Code <= 0 {
				continue
			}
			rp := map[string]any{}
			if r.Desc != "" {
				rp["description"] = r.Desc
			} else {
				rp["description"] = fmt.Sprintf("%d", r.Code)
			}
			content := map[string]any{}
			if len(r.Schema) > 0 && json.Valid([]byte(r.Schema)) {
				var schema any
				if err := json.Unmarshal([]byte(r.Schema), &schema); err == nil {
					content["schema"] = schema
				}
			}
			if r.Example != "" {
				var ex any
				if err := json.Unmarshal([]byte(r.Example), &ex); err == nil {
					content["example"] = ex
				}
			}
			if len(content) > 0 {
				rp["content"] = map[string]any{"application/json": content}
			}
			resps[fmt.Sprintf("%d", r.Code)] = rp
		}
		if len(resps) == 0 {
			resps["200"] = map[string]any{"description": "OK"}
		}
		op["responses"] = resps
		pathItem[strings.ToLower(a.Method)] = op
	}

	spec := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":   title,
			"version": "1.0.0",
		},
		"servers": []any{map[string]any{"url": "http://localhost" + commonPrefix}},
		"paths":   paths,
	}
	if description != "" {
		spec["info"].(map[string]any)["description"] = description
	}
	return spec
}

func paramMap(in, name, typ string, required bool, desc, example string) map[string]any {
	m := map[string]any{
		"in":     in,
		"name":   name,
		"schema": map[string]any{"type": typ},
	}
	if required && in != "path" {
		m["required"] = true
	}
	if desc != "" {
		m["description"] = desc
	}
	if example != "" {
		if in == "query" {
			m["example"] = example
		}
	}
	return m
}

// CommonPrefix 计算全部接口路径的公共前缀(段对齐)。
// 规则:
//   - 仅当 ≥2 条路径时才抽取(单接口无"公共"概念,保持原路径);
//   - 前缀不超过最短路径段数-1(保证导出的 path 不为空);
//   - 占位符段({x})不作为前缀。
func CommonPrefix(paths []string) string {
	if len(paths) < 2 {
		return ""
	}
	split := make([][]string, len(paths))
	minSegs := 1 << 30
	for i, p := range paths {
		split[i] = strings.Split(strings.Trim(p, "/"), "/")
		if p == "/" || p == "" {
			split[i] = nil
		}
		if len(split[i]) < minSegs {
			minSegs = len(split[i])
		}
	}
	if minSegs <= 0 {
		return ""
	}
	maxCommon := minSegs - 1
	if maxCommon <= 0 {
		return ""
	}
	var common []string
	for i := 0; i < maxCommon; i++ {
		seg := split[0][i]
		all := true
		for _, s := range split {
			if s[i] != seg || strings.HasPrefix(seg, "{") {
				all = false
				break
			}
		}
		if !all {
			break
		}
		common = append(common, seg)
	}
	if len(common) == 0 {
		return ""
	}
	return "/" + strings.Join(common, "/")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
