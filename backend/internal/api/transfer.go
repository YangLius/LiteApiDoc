// 导入导出处理器:OpenAPI3.0(分析/应用)、平台JSON备份、离线HTML。
package api

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
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

// specItem 分析结果条目(OpenAPIAnalyze 与 SpecURLAnalyze 共用)。
type specItem struct {
	Name         string `json:"name"`
	Method       string `json:"method"`
	Path         string `json:"path"`
	Exists       bool   `json:"exists"`
	ConflictPath string `json:"conflictPath"`
}

// analyzeSpecItems 对解析出的接口做存在性/模板冲突分析。
func (s *Server) analyzeSpecItems(projectID int64, basePath string, apis []openapi.ParsedApi) ([]specItem, int, int) {
	items := make([]specItem, 0, len(apis))
	for _, pa := range apis {
		full := joinBase(basePath, pa.Path)
		exists, _ := s.Store.ApiExistsByMethodPath(projectID, pa.Method, full)
		conflict, _ := s.Store.FindTemplateConflict(projectID, pa.Method, full)
		items = append(items, specItem{
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
	return items, toCreate, toOverwrite
}

// OpenAPIAnalyze POST /api/projects/:id/import/openapi/analyze {content}
// 支持 OpenAPI 3.0 与 Swagger 2.0(自动转换归一化),$ref 全量内联。
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
	content, kind, warnings, err := openapi.Normalize(req.Content)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	basePath, apis, title, err := openapi.Parse(content)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	items, toCreate, toOverwrite := s.analyzeSpecItems(p.ID, basePath, apis)
	ok(c, gin.H{
		"title": title, "basePath": basePath, "items": items,
		"toCreate": toCreate, "toOverwrite": toOverwrite, "total": len(items),
		"specKind": kind, "warnings": warnings, "content": content,
	})
}

// mustNormalized 归一化规范内容(Swagger2→OpenAPI3 + $ref 内联)。
// 失败时返回原文,让后续 Parse 报出具体版本错误,避免双重报错。
func mustNormalized(content string) string {
	nc, _, _, err := openapi.Normalize(content)
	if err != nil {
		return content
	}
	return nc
}

// specResult 单个地址的分析结果。
type specResult struct {
	Url         string     `json:"url"`
	SpecUrl     string     `json:"specUrl"`
	SpecKind    string     `json:"specKind"`
	Title       string     `json:"title"`
	BasePath    string     `json:"basePath"`
	Warnings    []string   `json:"warnings"`
	Items       []specItem `json:"items"`
	ToCreate    int        `json:"toCreate"`
	ToOverwrite int        `json:"toOverwrite"`
	Total       int        `json:"total"`
	AnalysisId  string     `json:"analysisId"`
	Error       string     `json:"error"`
}

// analyzeOneSpec 拉取并解析单个地址;内容存入缓存供 apply 凭 analysisId 引用。
func (s *Server) analyzeOneSpec(projectID int64, rawURL string, insecure bool, headers map[string]string) specResult {
	res := specResult{Url: rawURL}
	content, specURL, kind, warnings, err := fetchAndNormalizeSpec(rawURL, insecure, headers)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	basePath, apis, title, err := openapi.Parse(content)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	items, toCreate, toOverwrite := s.analyzeSpecItems(projectID, basePath, apis)
	res.SpecUrl, res.SpecKind, res.Title, res.BasePath = specURL, kind, title, basePath
	res.Warnings, res.Items = warnings, items
	res.ToCreate, res.ToOverwrite, res.Total = toCreate, toOverwrite, len(items)
	res.AnalysisId = specCachePut(content)
	return res
}

// firstSuccess 返回第一个成功结果;全失败时返回零值。
func firstSuccess(results []specResult) specResult {
	for _, r := range results {
		if r.Error == "" {
			return r
		}
	}
	return specResult{}
}

// SpecURLAnalyze POST /api/projects/:id/import/spec-url/analyze {url|urls, save, insecure, headers}
// 支持批量:urls 每项独立发现+解析(并发拉取,上限 4),返回 results[],内容存缓存
// (apply 凭 analysisId 引用,免回传大规范)。save=true 时把成功的规范地址与请求头
// 保存为项目同步源(多个地址存 JSON 数组)。headers 为空 map 清除已存请求头,nil 沿用。
func (s *Server) SpecURLAnalyze(c *gin.Context) {
	p, allowed := s.checkEdit(c, pathID(c, "id"))
	if !allowed {
		return
	}
	var req struct {
		URL      string            `json:"url"`  // 单地址(向后兼容)
		URLs     []string          `json:"urls"` // 多地址批量
		Save     bool              `json:"save"`
		Insecure bool              `json:"insecure"`
		Headers  map[string]string `json:"headers"` // 拉取规范时附带的请求头(如 Authorization)
	}
	if !bindJSON(c, &req) {
		return
	}
	urls := req.URLs
	if len(urls) == 0 && strings.TrimSpace(req.URL) != "" {
		urls = []string{req.URL}
	}
	cleaned := make([]string, 0, len(urls))
	for _, u := range urls {
		if u = strings.TrimSpace(u); u != "" {
			cleaned = append(cleaned, u)
		}
	}
	if len(cleaned) == 0 {
		fail(c, http.StatusBadRequest, "请填写服务地址或规范文档地址")
		return
	}
	effHeaders := req.Headers
	if effHeaders == nil && p.SpecHeaders != "" { // 未传则沿用已保存的同步源请求头
		var stored map[string]string
		if json.Unmarshal([]byte(p.SpecHeaders), &stored) == nil {
			effHeaders = stored
		}
	}
	results := make([]specResult, len(cleaned))
	type indexed struct {
		i   int
		res specResult
	}
	ch := make(chan indexed, len(cleaned)) // 缓冲足够,发送不阻塞
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, u := range cleaned {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ch <- indexed{i, s.analyzeOneSpec(p.ID, u, req.Insecure, effHeaders)}
		}(i, u)
	}
	wg.Wait()
	close(ch)
	for it := range ch { // 主协程单点写回,无数据竞争
		results[it.i] = it.res
	}
	if req.Save {
		saved := make([]string, 0, len(results))
		for _, r := range results {
			if r.Error == "" && r.SpecUrl != "" {
				saved = append(saved, r.SpecUrl)
			}
		}
		if len(saved) > 0 {
			np := p
			if len(saved) == 1 {
				np.SpecUrl = saved[0]
			} else if b, merr := json.Marshal(saved); merr == nil {
				np.SpecUrl = string(b) // 多同步源存 JSON 数组
			}
			if req.Headers != nil { // 显式传入才改;nil 保持原值,{} 清空
				if len(req.Headers) == 0 {
					np.SpecHeaders = ""
				} else if b, merr := json.Marshal(req.Headers); merr == nil {
					np.SpecHeaders = string(b)
				}
			}
			if err := s.Store.UpdateProject(&np); err != nil {
				fail(c, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	first := firstSuccess(results)
	ok(c, gin.H{
		"results": results,
		// 顶层兼容字段 = 第一个成功结果(单地址时与旧版一致)
		"specUrl": first.SpecUrl, "specKind": first.SpecKind, "title": first.Title,
		"basePath": first.BasePath, "warnings": first.Warnings, "items": first.Items,
		"toCreate": first.ToCreate, "toOverwrite": first.ToOverwrite, "total": first.Total,
		"analysisId": first.AnalysisId,
	})
}

// specCache spec-url 分析内容的内存缓存:analyze 返回 analysisId,apply 凭 ID
// 取内容,避免数 MB 的规范在前后端之间来回传输。15 分钟过期,惰性清理。
type specCacheEntry struct {
	content   string
	createdAt time.Time
}

var specCache sync.Map // analysisId -> specCacheEntry

func specCachePut(content string) string {
	b := make([]byte, 8)
	rand.Read(b) // 缓存 ID 仅会话内引用,无加密需求也无妨
	id := hex.EncodeToString(b)
	now := time.Now()
	specCache.Range(func(k, v any) bool {
		if e, ok := v.(specCacheEntry); ok && now.Sub(e.createdAt) > 15*time.Minute {
			specCache.Delete(k)
		}
		return true
	})
	specCache.Store(id, specCacheEntry{content: content, createdAt: now})
	return id
}

func specCacheGet(id string) (string, bool) {
	v, ok := specCache.Load(id)
	if !ok {
		return "", false
	}
	e := v.(specCacheEntry)
	if time.Since(e.createdAt) > 15*time.Minute {
		specCache.Delete(id)
		return "", false
	}
	return e.content, true
}

// specEndpoints 常见规范端点候选(按生态命中率排序)。
var specEndpoints = []string{
	"/swagger/doc.json", // Go swag
	"/v3/api-docs",      // springdoc (Spring Boot)
	"/openapi.json",     // FastAPI / 通用
	"/swagger.json",
	"/v2/swagger.json", // springfox
	"/api-docs",        // springfox 默认
	"/api/openapi.json",
}

// fetchAndNormalizeSpec 拉取并归一化规范;url 可为规范直链或服务基础地址;
// headers 为拉取时附带的请求头(受保护的服务,如 OAuth2 Bearer)。
func fetchAndNormalizeSpec(rawURL string, insecure bool, headers map[string]string) (content, specURL, kind string, warnings []string, err error) {
	u, perr := url.Parse(rawURL)
	if perr != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", "", "", nil, fmt.Errorf("仅支持 http/https 地址")
	}
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: insecure},
		},
	}
	fetch := func(target string) (string, error) {
		req, rerr := http.NewRequest(http.MethodGet, target, nil)
		if rerr != nil {
			return "", rerr
		}
		for k, v := range headers { // 认证头等随每个探测请求携带
			if k != "" {
				req.Header.Set(k, v)
			}
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
		if err != nil {
			return "", err
		}
		return string(body), nil
	}
	tryNormalize := func(body string) (string, string, []string, bool) {
		nc, kind, warns, err := openapi.Normalize(body)
		if err != nil {
			return "", "", nil, false
		}
		return nc, kind, warns, true
	}

	// 1) 直连:地址本身是规范文档
	if body, err := fetch(rawURL); err == nil {
		if content, kind, warns, ok := tryNormalize(body); ok {
			return content, rawURL, kind, warns, nil
		}
	}
	// 2) 基础地址探测常见规范端点
	base := strings.TrimSuffix(rawURL, "/")
	for _, c := range specEndpoints {
		target := base + c
		body, err := fetch(target)
		if err != nil {
			continue
		}
		if content, kind, warns, ok := tryNormalize(body); ok {
			return content, target, kind, warns, nil
		}
	}
	// 3) 文档页面发现:FastAPI /docs、swagger-ui 页面内写明真实规范地址
	if content, specURL, kind, warns, ok := discoverFromPages(base, fetch, tryNormalize); ok {
		return content, specURL, kind, warns, nil
	}
	return "", "", "", nil, fmt.Errorf(
		"未能从该地址拉取到规范(已尝试直连、常见端点及 /docs 页面自动发现),请确认服务已开启文档,或直接填写规范文档地址")
}

// discoveryPages swagger 文档页面候选(页面 HTML 内会写明真实规范地址)。
var discoveryPages = []string{
	"/docs",            // FastAPI 默认文档页(SwaggerUIBundle({url: ...}))
	"/redoc",            // FastAPI ReDoc 页(spec-url 属性)
	"/swagger-ui.html", // 传统 swagger-ui
	"/swagger-ui/",      // springdoc swagger-ui
}

var (
	reOpenapiURL  = regexp.MustCompile(`openapi_url["']?\s*[:=]\s*["']([^"']+)["']`)
	reSwaggerUIURL = regexp.MustCompile(`SwaggerUIBundle\(\{[^}]*?\b["']?url["']?\s*:\s*["']([^"']+)["']`)
	reReDocURL    = regexp.MustCompile(`spec-url=["']([^"']+)["']`)
	reConfigURL   = regexp.MustCompile(`configUrl["']?\s*[:=]\s*["']([^"']+)["']`)
	reSpecURL     = regexp.MustCompile(`"url"\s*:\s*"([^"]+)"`)
)

// resolveSpecRef 把页面里提取的地址解析为绝对地址;无法解析返回空。
func resolveSpecRef(base, ref string) string {
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	if strings.HasPrefix(ref, "/") {
		return base + ref
	}
	return ""
}

// discoverFromPages 拉取文档页面,从 HTML 中提取真实规范地址并验证。
func discoverFromPages(base string, fetch func(string) (string, error),
	tryNormalize func(string) (string, string, []string, bool)) (string, string, string, []string, bool) {
	seen := map[string]bool{}
	for _, page := range discoveryPages {
		html, err := fetch(base + page)
		if err != nil || !strings.Contains(html, "<") { // 只处理 HTML 页面
			continue
		}
		var cands []string
		if m := reOpenapiURL.FindStringSubmatch(html); m != nil { // 部分模板字面渲染 openapi_url 配置
			cands = append(cands, m[1])
		}
		if m := reSwaggerUIURL.FindStringSubmatch(html); m != nil { // FastAPI: SwaggerUIBundle({url: '/api/v1/openapi.json'})
			cands = append(cands, m[1])
		}
		if m := reReDocURL.FindStringSubmatch(html); m != nil { // ReDoc: <redoc spec-url="...">
			cands = append(cands, m[1])
		}
		if m := reConfigURL.FindStringSubmatch(html); m != nil { // springdoc: configUrl → {"url":"..."}
			if cfg, err := fetch(resolveSpecRef(base, m[1])); err == nil {
				if mm := reSpecURL.FindStringSubmatch(cfg); mm != nil {
					cands = append(cands, mm[1])
				}
			}
		}
		for _, c := range cands {
			target := resolveSpecRef(base, c)
			if target == "" || seen[target] {
				continue
			}
			seen[target] = true
			body, err := fetch(target)
			if err != nil {
				continue
			}
			if content, kind, warns, ok := tryNormalize(body); ok {
				return content, target, kind, warns, true
			}
		}
	}
	return "", "", "", nil, false
}

// OpenAPIApply POST /api/projects/:id/import/openapi/apply
// {content, decisions:[{method,path,action}]} action ∈ overwrite|skip,未列出按 skip。
func (s *Server) OpenAPIApply(c *gin.Context) {
	p, allowed := s.checkEdit(c, pathID(c, "id"))
	if !allowed {
		return
	}
	var req struct {
		Content    string `json:"content"`
		AnalysisID string `json:"analysisId"` // spec-url 分析缓存 ID(免回传大规范内容)
		GroupID    int64  `json:"groupId"`   // 导入目标分组(0=未分组;>0 时已有同路径接口也移入该组)
		Decisions []struct {
			Method string `json:"method"`
			Path   string `json:"path"`
			Action string `json:"action"`
		} `json:"decisions"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if req.Content == "" && req.AnalysisID != "" {
		if cached, ok := specCacheGet(req.AnalysisID); ok {
			req.Content = cached
		}
	}
	if req.Content == "" {
		fail(c, http.StatusBadRequest, "缺少规范内容或有效的分析ID")
		return
	}
	basePath, apis, _, err := openapi.Parse(mustNormalized(req.Content))
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
		na.GroupID = req.GroupID // 新接口落入指定分组(0=未分组)
		isUpdate := false
		existing, err := s.Store.FindApiByMethodPath(p.ID, na.Method, na.Path)
		if err == nil {
			isUpdate = true
			na.ID, na.SortOrder, na.CreatedAt = existing.ID, existing.SortOrder, existing.CreatedAt
			if req.GroupID <= 0 {
				na.GroupID = existing.GroupID // 未显式指定分组时,保留已有归属
			}
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
		Remark:     pa.Remark, // OpenAPI description → 接口备注
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
