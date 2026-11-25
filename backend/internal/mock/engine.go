// Package mock 实现 Mock HTTP 服务引擎。
// 核心规则:文档中配置的响应示例即为 Mock 默认返回。
package mock

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"liteapidoc/internal/genexample"
	"liteapidoc/internal/model"
	"liteapidoc/internal/store"
)

// Engine Mock 服务。
type Engine struct {
	Store *store.Store

	corsMu   sync.Mutex
	corsList []string
	corsAt   time.Time
	corsDone bool
}

// Register 挂载 Mock 路由(mock 端口服务)。
func (e *Engine) Register(r *gin.Engine) {
	r.Use(e.applyCORS)
	r.GET("/mock/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.Any("/mock/:projectId/*path", e.handle)
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found", "path": c.Request.URL.Path})
	})
}

// applyCORS CORS 处理(PRD 模块 4.1:支持浏览器直接调用 Mock)。
// 策略:
//  1. 平台设置 mock_cors_origins(逗号分隔白名单)已配置 → 仅回显名单内来源;
//  2. 未配置 → 回显请求 Origin(满足内网任意开发前端开箱即用联调)。
// 始终不发送 Access-Control-Allow-Credentials:Mock 端点无鉴权、不依赖 Cookie,
// 不存在跨域凭证读取风险;Vary: Origin 防止中间代理缓存错误策略。
func (e *Engine) applyCORS(c *gin.Context) {
	h := c.Writer.Header()
	h.Add("Vary", "Origin")
	origin := c.GetHeader("Origin")
	if origin == "" {
		return // 同源或非浏览器请求,无需 CORS 头
	}
	if list := e.corsOrigins(); len(list) > 0 {
		hit := false
		for _, o := range list {
			if o != "" && strings.EqualFold(o, origin) {
				hit = true
				break
			}
		}
		if !hit {
			return // 来源不在白名单:不发 CORS 头,浏览器拒绝读取
		}
	}
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
	if rh := c.GetHeader("Access-Control-Request-Headers"); rh != "" {
		h.Set("Access-Control-Allow-Headers", rh) // 按预检请求回显,最小授权
	} else {
		h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
	}
	h.Set("Access-Control-Max-Age", "86400")
}

// corsOrigins 读取 CORS 白名单设置,5 秒缓存避免每请求查库。
func (e *Engine) corsOrigins() []string {
	e.corsMu.Lock()
	defer e.corsMu.Unlock()
	if e.corsDone && time.Since(e.corsAt) < 5*time.Second {
		return e.corsList
	}
	v, _ := e.Store.GetSetting("mock_cors_origins")
	var list []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			list = append(list, p)
		}
	}
	e.corsList, e.corsAt, e.corsDone = list, time.Now(), true
	return list
}

func (e *Engine) handle(c *gin.Context) {
	// CORS 预检:任何 /mock 路径的 OPTIONS 直接放行
	if c.Request.Method == http.MethodOptions {
		c.AbortWithStatus(http.StatusNoContent)
		return
	}
	start := time.Now()
	projectID := parseID(c.Param("projectId"))
	reqPath := c.Param("path")
	if !strings.HasPrefix(reqPath, "/") {
		reqPath = "/" + reqPath
	}
	method := strings.ToUpper(c.Request.Method)
	query := c.Request.URL.RawQuery

	writeLog := func(code int) {
		// 日志写入失败不影响 Mock 响应,刻意忽略错误
		_ = e.Store.InsertMockLog(&model.MockLog{
			ProjectID: projectID, Method: method, Path: reqPath,
			Query: query, RespCode: code, DurationMS: time.Since(start).Milliseconds(),
		})
	}

	// 项目不存在
	p, err := e.Store.GetProject(projectID)
	if err != nil {
		writeLog(http.StatusNotFound)
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found", "projectId": projectID})
		return
	}

	// 总开关关闭 → 统一 503,与 404 区分(PRD 模块4.3)
	if !p.MockEnabled {
		writeLog(http.StatusServiceUnavailable)
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "mock disabled for this project", "code": "MOCK_DISABLED",
		})
		return
	}

	apis, err := e.Store.ListApis(projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	api := matchApi(apis, method, reqPath)
	if api == nil {
		writeLog(http.StatusNotFound)
		c.JSON(http.StatusNotFound, gin.H{
			"error": "api not found", "method": method, "path": reqPath,
		})
		return
	}

	// 废弃接口 Mock 行为(PRD 业务规则2)
	if api.Status == model.ApiStatusDeprecated {
		switch api.Mock.DeprecatedMode {
		case model.Deprecated404:
			writeLog(http.StatusNotFound)
			c.JSON(http.StatusNotFound, gin.H{"error": "api deprecated", "path": reqPath})
			return
		case model.DeprecatedTip:
			writeLog(http.StatusGone)
			c.JSON(http.StatusGone, gin.H{"code": 410, "message": "接口已废弃", "path": reqPath})
			return
		}
	}

	// 计算响应码与响应体
	code, body := resolveResponse(api, p)

	// 全局 Mock 返回 Header(PRD 模块4.3)
	for _, h := range p.GlobalMockHeaders {
		if h.Key != "" {
			c.Writer.Header().Set(h.Key, h.Value)
		}
	}
	if c.Writer.Header().Get("Content-Type") == "" {
		c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	}

	// 延时模拟(0~5000ms)
	if d := api.Mock.DelayMS; d > 0 {
		if d > 5000 {
			d = 5000
		}
		time.Sleep(time.Duration(d) * time.Millisecond)
	}

	writeLog(code)
	c.Data(code, c.Writer.Header().Get("Content-Type"), []byte(body))
}

// matchApi 路由匹配:先精确匹配;未命中按路径模板匹配(占位符匹配单段非 / 字符),
// 多个模板命中时固定路径段多者优先(PRD 模块4.1)。
func matchApi(apis []model.Api, method, reqPath string) *model.Api {
	var best *model.Api
	bestFixed := -1
	for i := range apis {
		a := &apis[i]
		if a.Method != method {
			continue
		}
		if a.Path == reqPath {
			return a // 精确匹配最优先
		}
		if fixed, ok := templateMatch(a.Path, reqPath); ok && fixed > bestFixed {
			best = a
			bestFixed = fixed
		}
	}
	return best
}

// templateMatch 判断请求路径是否命中模板,返回固定段数量。
func templateMatch(tpl, reqPath string) (int, bool) {
	ts := splitSegs(tpl)
	rs := splitSegs(reqPath)
	if len(ts) != len(rs) {
		return 0, false
	}
	fixed := 0
	for i, seg := range ts {
		if isPlaceholder(seg) {
			if rs[i] == "" {
				return 0, false
			}
			continue
		}
		if seg != rs[i] {
			return 0, false
		}
		fixed++
	}
	return fixed, true
}

func splitSegs(p string) []string {
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return []string{}
	}
	return strings.Split(trimmed, "/")
}

func isPlaceholder(seg string) bool {
	return len(seg) >= 2 && seg[0] == '{' && seg[len(seg)-1] == '}'
}

func parseID(s string) int64 {
	var id int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		id = id*10 + int64(r-'0')
	}
	return id
}

// resolveResponse 计算响应码与响应体:
//   - 强制响应码:优先取文档中同码场景示例,无场景时空 Body(PRD 模块4.2)
//   - 自定义返回 JSON:默认策略下优先(PRD 模块4.2)
//   - 默认:返回成功示例(优先 200 场景,否则响应码最小的场景)
func resolveResponse(api *model.Api, p model.Project) (int, string) {
	mc := api.Mock

	// 找响应场景:优先 200,否则响应码最小者
	pick := func() *model.ResponseDef {
		var fallback *model.ResponseDef
		for i := range api.Responses {
			r := &api.Responses[i]
			if r.Code == http.StatusOK {
				return r
			}
			if fallback == nil || r.Code < fallback.Code {
				fallback = r
			}
		}
		return fallback
	}
	exampleOf := func(r *model.ResponseDef) string {
		if r == nil {
			return ""
		}
		if strings.TrimSpace(r.Example) != "" {
			return r.Example
		}
		// 示例缺失时按 Schema 生成
		return genexample.FromSchemaString(string(r.Schema))
	}

	if mc.ForceCode > 0 {
		for i := range api.Responses {
			if api.Responses[i].Code == mc.ForceCode {
				return mc.ForceCode, exampleOf(&api.Responses[i])
			}
		}
		return mc.ForceCode, ""
	}
	if strings.TrimSpace(mc.CustomJSON) != "" {
		return http.StatusOK, mc.CustomJSON
	}
	if body := exampleOf(pick()); body != "" {
		return http.StatusOK, body
	}
	// 完全没有响应定义:返回请求 body 示例(若有)或空 JSON
	if strings.TrimSpace(api.BodyExample) != "" {
		return http.StatusOK, api.BodyExample
	}
	return http.StatusOK, "{}"
}
