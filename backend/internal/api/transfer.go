// 导入导出处理器:OpenAPI3.0(分析/应用)、平台JSON备份、离线HTML。
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

	"liteapidoc/internal/model"
	"liteapidoc/internal/openapi"
)

var fileNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeFileName(name, fallback string) string {
	s := fileNameUnsafe.ReplaceAllString(strings.TrimSpace(name), "-")
	s = strings.Trim(s, "-.")
	if s == "" {
		return fallback
	}
	return s
}

// joinBase 将 servers basePath 拼接到接口路径(PRD 模块7)。
func joinBase(base, p string) string {
	base = strings.TrimSuffix(strings.TrimSpace(base), "/")
	if base == "" || base == "/" {
		if !strings.HasPrefix(p, "/") {
			return "/" + p
		}
		return p
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return base + p
}

// ---- OpenAPI 导入(两阶段:analyze 预检冲突 → apply 按决策写入) ----

// OpenAPIAnalyze POST /api/projects/:id/import/openapi/analyze {content}
func (s *Server) OpenAPIAnalyze(c *gin.Context) {
	p, allowed := s.checkEdit(c, pathID(c, "id"))
	if !allowed {
		return
	}
	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if !bindJSON(c, &req) {
		return
	}
	basePath, apis, title, err := openapi.Parse(req.Content)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	type item struct {
		Name         string `json:"name"`
		Method       string `json:"method"`
		Path         string `json:"path"`
		Exists       bool   `json:"exists"`
		ConflictPath string `json:"conflictPath"`
	}
	items := make([]item, 0, len(apis))
	for _, pa := range apis {
		full := joinBase(basePath, pa.Path)
		exists, _ := s.Store.ApiExistsByMethodPath(p.ID, pa.Method, full)
		conflict, _ := s.Store.FindTemplateConflict(p.ID, pa.Method, full)
		items = append(items, item{
			Name: pa.Name, Method: pa.Method, Path: full,
			Exists: exists, ConflictPath: conflict,
		})
	}
	toCreate, toOverwrite := 0, 0
	for _, it := range items {
		if it.Exists {
			toOverwrite++
		} else {
			toCreate++
		}
	}
	ok(c, gin.H{
		"title": title, "basePath": basePath, "items": items,
		"toCreate": toCreate, "toOverwrite": toOverwrite, "total": len(items),
	})
}

// OpenAPIApply POST /api/projects/:id/import/openapi/apply
// {content, decisions:[{method,path,action}]} action ∈ overwrite|skip,未列出按 skip。
func (s *Server) OpenAPIApply(c *gin.Context) {
	p, allowed := s.checkEdit(c, pathID(c, "id"))
	if !allowed {
		return
	}
	var req struct {
		Content   string `json:"content" binding:"required"`
		Decisions []struct {
			Method string `json:"method"`
			Path   string `json:"path"`
			Action string `json:"action"`
		} `json:"decisions"`
	}
	if !bindJSON(c, &req) {
		return
	}
	basePath, apis, _, err := openapi.Parse(req.Content)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	act := map[string]string{}
	for _, d := range req.Decisions {
		act[d.Method+" "+d.Path] = strings.ToLower(d.Action)
	}
	created, updated, skipped := 0, 0, 0
	var conflicts []string
	for _, pa := range apis {
		full := joinBase(basePath, pa.Path)
		if act[pa.Method+" "+full] != "overwrite" {
			skipped++
			continue
		}
		na := parsedToApi(p.ID, basePath, pa)
		isUpdate := false
		existing, err := s.Store.FindApiByMethodPath(p.ID, na.Method, na.Path)
		if err == nil {
			isUpdate = true
			na.ID, na.GroupID, na.SortOrder, na.CreatedAt = existing.ID, existing.GroupID, existing.SortOrder, existing.CreatedAt
			err = s.Store.UpdateApi(&na)
		} else {
			err = s.Store.CreateApi(&na)
		}
		switch {
		case err == nil:
			if isUpdate {
				updated++
			} else {
				created++
			}
		case strings.Contains(err.Error(), "模板冲突"):
			conflicts = append(conflicts, na.Method+" "+na.Path)
		default:
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	ok(c, gin.H{"created": created, "updated": updated, "skipped": skipped, "conflicts": conflicts})
}

// parsedToApi 将解析结果转为存储模型。
func parsedToApi(projectID int64, basePath string, pa openapi.ParsedApi) model.Api {
	a := model.Api{
		ProjectID:  projectID,
		Name:       pa.Name,
		Method:     pa.Method,
		Path:       joinBase(basePath, pa.Path),
		Status:     model.ApiStatusDeveloping,
		PathParams: pa.PathParams,
		QueryParams: pa.QueryParams,
		HeaderParams: pa.HeaderParams,
		Mock:       model.MockConfig{DeprecatedMode: model.DeprecatedNormal},
	}
	if len(pa.BodySchema) > 0 {
		a.BodySchema = []byte(pa.BodySchema)
	}
	a.Responses = pa.Responses
	// 保证非 null(便于前端处理)
	if a.PathParams == nil {
		a.PathParams = []model.Param{}
	}
	if a.QueryParams == nil {
		a.QueryParams = []model.Param{}
	}
	if a.HeaderParams == nil {
		a.HeaderParams = []model.HeaderParam{}
	}
	if a.Responses == nil {
		a.Responses = []model.ResponseDef{}
	}
	return a
}

// ---- OpenAPI 导出 ----

// ExportOpenAPI GET /api/projects/:id/export/openapi?format=yaml|json
func (s *Server) ExportOpenAPI(c *gin.Context) {
	p, allowed := s.checkEdit(c, pathID(c, "id"))
	if !allowed {
		return
	}
	format := strings.ToLower(c.DefaultQuery("format", "yaml"))
	apis, err := s.Store.ListApis(p.ID)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	groups, _ := s.Store.ListGroups(p.ID)
	paths := make([]string, 0, len(apis))
	for _, a := range apis {
		paths = append(paths, a.Path)
	}
	prefix := openapi.CommonPrefix(paths)
	spec := openapi.Build(apis, groups, p.Name, p.Description, prefix)

	var body []byte
	var ext, ctype string
	if format == "json" {
		body, _ = json.MarshalIndent(spec, "", "  ")
		ext, ctype = "json", "application/json; charset=utf-8"
	} else {
		body, _ = yaml.Marshal(spec)
		ext, ctype = "yaml", "application/yaml; charset=utf-8"
	}
	// PRD 模块7:Mock 专属配置(延时/强制错误码/自定义返回)无法映射 OpenAPI,导出丢弃,由前端提示
	c.Header("X-Mock-Fields-Dropped", "delayMs,forceCode,customJson,deprecatedMode")
	c.Header("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s-openapi.%s"`, safeFileName(p.Name, "project"), ext))
	c.Data(http.StatusOK, ctype, body)
}

// ---- 平台自有格式 JSON 备份 ----

type platformBackup struct {
	SchemaVersion int             `json:"schemaVersion"`
	ExportedAt    int64           `json:"exportedAt"`
	Project       backupProject   `json:"project"`
	Groups        []model.Group   `json:"groups"`
	Apis          []model.Api     `json:"apis"`
	Envs          []model.Env     `json:"envs"`
}

type backupProject struct {
	Name              string     `json:"name"`
	Description       string     `json:"description"`
	Tags              []string   `json:"tags"`
	MockEnabled       bool       `json:"mockEnabled"`
	GlobalHeaders     []model.KV `json:"globalHeaders"`
	GlobalMockHeaders []model.KV `json:"globalMockHeaders"`
}

// ExportJSON GET /api/projects/:id/export/json
func (s *Server) ExportJSON(c *gin.Context) {
	p, allowed := s.checkEdit(c, pathID(c, "id"))
	if !allowed {
		return
	}
	groups, _ := s.Store.ListGroups(p.ID)
	apis, _ := s.Store.ListApis(p.ID)
	envs, _ := s.Store.ListEnvs(p.ID)
	bk := platformBackup{
		SchemaVersion: 1,
		ExportedAt:    time.Now().Unix(),
		Project: backupProject{
			Name: p.Name, Description: p.Description, Tags: p.Tags,
			MockEnabled: p.MockEnabled, GlobalHeaders: p.GlobalHeaders, GlobalMockHeaders: p.GlobalMockHeaders,
		},
		Groups: groups, Apis: apis, Envs: envs,
	}
	body, _ := json.MarshalIndent(bk, "", "  ")
	c.Header("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s-backup.json"`, safeFileName(p.Name, "project")))
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

// ImportProjectJSON POST /api/admin/import/project {content} — 由备份创建新项目(管理员)
func (s *Server) ImportProjectJSON(c *gin.Context) {
	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if !bindJSON(c, &req) {
		return
	}
	var bk platformBackup
	if err := json.Unmarshal([]byte(req.Content), &bk); err != nil {
		fail(c, http.StatusBadRequest, "备份文件解析失败: "+err.Error())
		return
	}
	if bk.Project.Name == "" {
		fail(c, http.StatusBadRequest, "备份缺少项目名称")
		return
	}
	np := model.Project{
		Name: bk.Project.Name + "(导入)", Description: bk.Project.Description, Tags: bk.Project.Tags,
		MockEnabled: bk.Project.MockEnabled, GlobalHeaders: bk.Project.GlobalHeaders, GlobalMockHeaders: bk.Project.GlobalMockHeaders,
	}
	if err := s.Store.CreateProject(&np, ""); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	// 分组:先一级后二级,重映射 ID
	oldToNew := map[int64]int64{}
	sort.Slice(bk.Groups, func(i, j int) bool {
		if (bk.Groups[i].ParentID == 0) != (bk.Groups[j].ParentID == 0) {
			return bk.Groups[i].ParentID == 0
		}
		return bk.Groups[i].ID < bk.Groups[j].ID
	})
	for _, g := range bk.Groups {
		ng := model.Group{ProjectID: np.ID, Name: g.Name, ParentID: oldToNew[g.ParentID], SortOrder: g.SortOrder}
		if err := s.Store.CreateGroup(&ng); err == nil {
			oldToNew[g.ID] = ng.ID
		}
	}
	// 接口
	warnings := []string{}
	for _, a := range bk.Apis {
		a.ID = 0
		a.ProjectID = np.ID
		a.GroupID = oldToNew[a.GroupID]
		if err := sanitizeApi(&a); err != nil {
			warnings = append(warnings, a.Name+": "+err.Error())
			continue
		}
		if err := s.Store.CreateApi(&a); err != nil {
			warnings = append(warnings, a.Method+" "+a.Path+": 导入失败")
		}
	}
	// 环境
	for _, e := range bk.Envs {
		e.ID = 0
		e.ProjectID = np.ID
		_ = s.Store.CreateEnv(&e)
	}
	ok(c, gin.H{"projectId": np.ID, "warnings": warnings})
}

// ---- 离线静态 HTML 导出 ----

type htmlGroup struct {
	ID       int64
	Name     string
	Apis     []model.Api
	Children []htmlGroup
}

// ExportHTML GET /api/projects/:id/export/html
func (s *Server) ExportHTML(c *gin.Context) {
	p, allowed := s.checkEdit(c, pathID(c, "id"))
	if !allowed {
		return
	}
	groups, _ := s.Store.ListGroups(p.ID)
	apis, _ := s.Store.ListApis(p.ID)

	apisOf := map[int64][]model.Api{}
	var ungrouped []model.Api
	for _, a := range apis {
		if a.GroupID == 0 {
			ungrouped = append(ungrouped, a)
			continue
		}
		apisOf[a.GroupID] = append(apisOf[a.GroupID], a)
	}
	childrenOf := map[int64][]model.Group{}
	var roots []model.Group
	for _, g := range groups {
		if g.ParentID == 0 {
			roots = append(roots, g)
		} else {
			childrenOf[g.ParentID] = append(childrenOf[g.ParentID], g)
		}
	}
	var build func(model.Group) htmlGroup
	build = func(g model.Group) htmlGroup {
		hg := htmlGroup{ID: g.ID, Name: g.Name, Apis: apisOf[g.ID]}
		for _, ch := range childrenOf[g.ID] {
			hg.Children = append(hg.Children, build(ch))
		}
		return hg
	}
	hgs := make([]htmlGroup, 0, len(roots))
	for _, r := range roots {
		hgs = append(hgs, build(r))
	}

	data := gin.H{
		"Title":       p.Name + " 接口文档",
		"Description": p.Description,
		"GeneratedAt": time.Now().Format("2006-01-02 15:04:05"),
		"Groups":      hgs,
		"Ungrouped":   ungrouped,
		"ApiCount":    len(apis),
	}
	tmpl, err := template.New("doc").Funcs(template.FuncMap{
		"pretty": func(s string) string {
			var out bytes.Buffer
			if json.Indent(&out, []byte(s), "", "  ") == nil {
				return out.String()
			}
			return s
		},
		"nonEmpty": func(s string) bool { return strings.TrimSpace(s) != "" },
	}).Parse(offlineHTMLTemplate)
	if err != nil {
		fail(c, http.StatusInternalServerError, "模板错误: "+err.Error())
		return
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		fail(c, http.StatusInternalServerError, "渲染失败: "+err.Error())
		return
	}
	c.Header("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s-doc.html"`, safeFileName(p.Name, "project")))
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(buf.String()))
}

const offlineHTMLTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
<style>
 body{font-family:-apple-system,"Segoe UI","Microsoft YaHei",sans-serif;margin:0;color:#24292f;background:#f6f8fa}
 header{background:#1f2328;color:#fff;padding:24px 32px}
 header h1{margin:0 0 8px;font-size:22px}
 header .meta{color:#8b949e;font-size:13px}
 main{max-width:1000px;margin:0 auto;padding:24px 16px}
 .group{background:#fff;border:1px solid #d0d7de;border-radius:8px;margin-bottom:20px;overflow:hidden}
 .group>h2{margin:0;padding:12px 20px;background:#f6f8fa;border-bottom:1px solid #d0d7de;font-size:16px}
 .sub{padding-left:20px;border-top:1px dashed #d0d7de}
 .api{padding:14px 20px;border-bottom:1px solid #eaeef2}
 .api:last-child{border-bottom:none}
 .api h3{margin:0 0 6px;font-size:15px}
 .badge{display:inline-block;padding:2px 8px;border-radius:4px;font-size:12px;font-weight:700;color:#fff;margin-right:8px}
 .GET{background:#22c55e}.POST{background:#3b82f6}.PUT{background:#f59e0b}.DELETE{background:#ef4444}.PATCH{background:#a855f7}
 .path{font-family:Consolas,monospace;background:#eff2f5;padding:2px 6px;border-radius:4px;font-size:13px}
 .deprecated{color:#9a6700;font-size:12px;border:1px solid #d4a72c;border-radius:4px;padding:1px 6px;margin-left:8px}
 table{border-collapse:collapse;width:100%;font-size:13px;margin:8px 0}
 th,td{border:1px solid #d0d7de;padding:5px 9px;text-align:left}
 th{background:#f6f8fa}
 pre{background:#0d1117;color:#e6edf3;padding:12px;border-radius:6px;overflow:auto;font-size:12px}
 .sec{margin-top:10px}
 .sec b{font-size:13px}
 .desc{color:#57606a;font-size:13px;white-space:pre-wrap}
</style>
</head>
<body>
<header>
  <h1>{{.Title}}</h1>
  <div class="meta">共 {{.ApiCount}} 个接口 · 生成于 {{.GeneratedAt}} · 由 LiteApiDoc 导出(离线只读,无 Mock)</div>
</header>
<main>
{{if .Description}}<p class="desc">{{.Description}}</p>{{end}}
{{define "apiBlock"}}
<div class="api">
  <h3><span class="badge {{.Method}}">{{.Method}}</span><span class="path">{{.Path}}</span>
  {{if eq .Status "deprecated"}}<span class="deprecated">已废弃</span>{{end}}</h3>
  <div>{{.Name}}{{if .Owner}} · 负责人:{{.Owner}}{{end}}</div>
  {{if .Remark}}<div class="desc">{{.Remark}}</div>{{end}}
  {{if .PathParams}}
  <div class="sec"><b>Path 参数</b><table><tr><th>名称</th><th>类型</th><th>必填</th><th>说明</th><th>示例</th></tr>
  {{range .PathParams}}<tr><td>{{.Name}}</td><td>{{.Type}}</td><td>{{if .Required}}是{{else}}否{{end}}</td><td>{{.Desc}}</td><td>{{.Example}}</td></tr>{{end}}</table></div>
  {{end}}
  {{if .QueryParams}}
  <div class="sec"><b>Query 参数</b><table><tr><th>名称</th><th>类型</th><th>必填</th><th>说明</th><th>示例</th></tr>
  {{range .QueryParams}}<tr><td>{{.Name}}</td><td>{{.Type}}</td><td>{{if .Required}}是{{else}}否{{end}}</td><td>{{.Desc}}</td><td>{{.Example}}</td></tr>{{end}}</table></div>
  {{end}}
  {{if .HeaderParams}}
  <div class="sec"><b>Header 参数</b><table><tr><th>Key</th><th>示例值</th><th>备注</th></tr>
  {{range .HeaderParams}}<tr><td>{{.Key}}</td><td>{{.Value}}</td><td>{{.Remark}}</td></tr>{{end}}</table></div>
  {{end}}
  {{if .Remark}}{{end}}
  {{$api := .}}
  {{if nonEmpty $api.BodyExample}}
  <div class="sec"><b>请求 Body 示例</b><pre>{{pretty $api.BodyExample}}</pre></div>
  {{end}}
  {{if .Responses}}
  <div class="sec"><b>响应定义</b></div>
  {{range .Responses}}
    <div class="sec"><b>{{.Code}}</b> {{.Desc}}
    {{if nonEmpty .Example}}<pre>{{pretty .Example}}</pre>{{else if .Schema}}<pre>{{pretty (printf "%s" .Schema)}}</pre>{{end}}</div>
  {{end}}
  {{end}}
</div>
{{end}}
{{range .Groups}}
<div class="group"><h2>{{.Name}}</h2>
  {{range .Apis}}{{template "apiBlock" .}}{{end}}
  {{range .Children}}
  <div class="sub group"><h2>{{.Name}}</h2>{{range .Apis}}{{template "apiBlock" .}}{{end}}</div>
  {{end}}
</div>
{{end}}
{{if .Ungrouped}}
<div class="group"><h2>未分组</h2>{{range .Ungrouped}}{{template "apiBlock" .}}{{end}}</div>
{{end}}
</main>
</body>
</html>`
