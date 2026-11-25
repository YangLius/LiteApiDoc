// 平台运营类处理器:分享链接、调试代理、全局设置、管理员操作。
package api

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"liteapidoc/internal/auth"
	"liteapidoc/internal/model"
)

// ---- 分享 ----

// ShareUpsert PUT /api/projects/:id/share
// {enabled, password(null=不变,空串=清除,非空=设置), resetToken}
func (s *Server) ShareUpsert(c *gin.Context) {
	p, allowed := s.checkEdit(c, pathID(c, "id"))
	if !allowed {
		return
	}
	var req struct {
		Enabled   bool     `json:"enabled"`
		Password  *string  `json:"password"`
		ResetToken bool    `json:"resetToken"`
	}
	if !bindJSON(c, &req) {
		return
	}
	hash, _ := s.Store.GetSharePasswordHash(p.ID)
	if req.Password != nil {
		if *req.Password == "" {
			hash = ""
		} else {
			var err error
			if hash, err = auth.HashPassword(*req.Password); err != nil {
				fail(c, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}
	sh := model.Share{ProjectID: p.ID, Enabled: req.Enabled}
	old, err := s.Store.GetShareByProject(p.ID)
	if err == nil && old.Token != "" && !req.ResetToken {
		sh.Token = old.Token // 复用原链接,避免已分发的链接失效
	} else {
		sh.Token = auth.GenSecret()[:32]
	}
	if err := s.Store.UpsertShare(&sh, hash); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"token": sh.Token, "enabled": sh.Enabled, "hasPassword": hash != ""})
}

// ShareGet GET /api/projects/:id/share(编辑权可见 token)
func (s *Server) ShareGet(c *gin.Context) {
	p, allowed := s.checkEdit(c, pathID(c, "id"))
	if !allowed {
		return
	}
	sh, err := s.Store.GetShareByProject(p.ID)
	if err != nil {
		ok(c, gin.H{"enabled": false, "hasPassword": false, "token": ""})
		return
	}
	ok(c, gin.H{"token": sh.Token, "enabled": sh.Enabled, "hasPassword": sh.HasPassword})
}

// ShareDelete DELETE /api/projects/:id/share 彻底移除分享配置(链接立即失效)
func (s *Server) ShareDelete(c *gin.Context) {
	p, allowed := s.checkEdit(c, pathID(c, "id"))
	if !allowed {
		return
	}
	if err := s.Store.DeleteShare(p.ID); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"deleted": true})
}

// ShareInfo GET /api/share/:token 公开:返回基本信息,带密码且未解锁时仅提示 locked
func (s *Server) ShareInfo(c *gin.Context) {
	token := c.Param("token")
	sh, err := s.Store.GetShareByToken(token)
	if err != nil || !sh.Enabled {
		fail(c, http.StatusNotFound, "分享链接不存在或已失效")
		return
	}
	p, err := s.Store.GetProject(sh.ProjectID)
	if err != nil {
		fail(c, http.StatusNotFound, "分享链接不存在或已失效")
		return
	}
	locked := sh.HasPassword
	if locked && !s.shareUnlocked(c, sh) {
		ok(c, gin.H{"locked": true, "name": p.Name})
		return
	}
	ok(c, gin.H{"locked": false, "name": p.Name, "description": p.Description})
}

// ShareUnlock POST /api/share/:token/unlock {password} → 分享访问 token
func (s *Server) ShareUnlock(c *gin.Context) {
	token := c.Param("token")
	lockKey := "share:" + token
	var req struct {
		Password string `json:"password" binding:"required"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if err := s.Lockout.Check(lockKey); err != nil {
		fail(c, http.StatusTooManyRequests, err.Error())
		return
	}
	sh, err := s.Store.GetShareByToken(token)
	if err != nil || !sh.Enabled {
		fail(c, http.StatusNotFound, "分享链接不存在或已失效")
		return
	}
	if !sh.HasPassword {
		ok(c, gin.H{"token": ""}) // 无密码分享无需凭证
		return
	}
	hash, err := s.Store.GetSharePasswordHash(sh.ProjectID)
	if err != nil || !auth.CheckPassword(hash, req.Password) {
		s.Lockout.Fail(lockKey)
		fail(c, http.StatusUnauthorized, "分享密码错误")
		return
	}
	s.Lockout.Reset(lockKey)
	ok(c, gin.H{"token": s.TM.Sign(auth.KindShare, sh.ProjectID)})
}

// ShareDoc GET /api/share/:token/doc 只读文档数据
func (s *Server) ShareDoc(c *gin.Context) {
	token := c.Param("token")
	sh, err := s.Store.GetShareByToken(token)
	if err != nil || !sh.Enabled {
		fail(c, http.StatusNotFound, "分享链接不存在或已失效")
		return
	}
	if sh.HasPassword && !s.shareUnlocked(c, sh) {
		fail(c, http.StatusUnauthorized, "请先输入分享密码")
		return
	}
	p, err := s.Store.GetProject(sh.ProjectID)
	if err != nil {
		fail(c, http.StatusNotFound, "项目不存在")
		return
	}
	groups, _ := s.Store.ListGroups(p.ID)
	apis, _ := s.Store.ListApis(p.ID)
	if groups == nil {
		groups = []model.Group{}
	}
	if apis == nil {
		apis = []model.Api{}
	}
	ok(c, gin.H{
		"project":  gin.H{"id": p.ID, "name": p.Name, "description": p.Description},
		"groups":   groups,
		"apis":     apis,
		"mockPath": fmt.Sprintf("/mock/%d", p.ID),
	})
}

// shareUnlocked 校验分享访问凭证(无密码分享恒真)。
func (s *Server) shareUnlocked(c *gin.Context, sh model.Share) bool {
	if !sh.HasPassword {
		return true
	}
	id, err := s.TM.Verify(tokenFrom(c), auth.KindShare)
	return err == nil && id == sh.ProjectID
}

// ---- 调试代理 ----

// DebugProxy POST /api/debug — 服务端代理转发,规避浏览器 CORS 与自签证书问题(PRD 模块5)。
func (s *Server) DebugProxy(c *gin.Context) {
	var req struct {
		ProjectID   int64             `json:"projectId"`
		Method      string            `json:"method"`
		URL         string            `json:"url"`
		Headers     map[string]string `json:"headers"`
		Query       map[string]string `json:"query"`
		Body        string            `json:"body"`
		TimeoutMS   int               `json:"timeoutMs"`
		InsecureTLS bool              `json:"insecureTls"`
	}
	if !bindJSON(c, &req) {
		return
	}
	// 需要项目编辑权(访客不可借代理发任意请求)
	if _, allowed := s.checkEdit(c, req.ProjectID); !allowed {
		return
	}
	req.Method = strings.ToUpper(strings.TrimSpace(req.Method))
	if !validMethods[req.Method] {
		fail(c, http.StatusBadRequest, "请求方式不合法")
		return
	}
	u, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		fail(c, http.StatusBadRequest, "目标 URL 不合法(仅支持 http/https)")
		return
	}
	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if timeout > 30*time.Second {
		timeout = 30 * time.Second
	}

	var body io.Reader
	if req.Body != "" {
		body = strings.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(c.Request.Context(), req.Method, u.String(), body)
	if err != nil {
		fail(c, http.StatusBadRequest, "构造请求失败: "+err.Error())
		return
	}
	for k, v := range req.Headers {
		if strings.EqualFold(k, "Host") {
			continue
		}
		httpReq.Header.Set(k, v)
	}
	if req.Body != "" && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if httpReq.Header.Get("User-Agent") == "" {
		httpReq.Header.Set("User-Agent", "LiteApiDoc-Debug/1.0")
	}
	q := httpReq.URL.Query()
	for k, v := range req.Query {
		q.Set(k, v)
	}
	httpReq.URL.RawQuery = q.Encode()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if req.InsecureTLS {
		//nolint:gosec // 内网自签证书为 PRD 场景,用户显式勾选;显式设 TLS1.2 下限
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
		}
	}
	client := &http.Client{Transport: transport, Timeout: timeout}

	start := time.Now()
	resp, err := client.Do(httpReq)
	if err != nil {
		ok(c, gin.H{"error": err.Error(), "durationMs": time.Since(start).Milliseconds()})
		return
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	size := int64(len(raw))
	bodyStr := string(raw)
	hdrs := map[string][]string{}
	for k, vs := range resp.Header {
		hdrs[k] = vs
	}
	ok(c, gin.H{
		"status":     resp.StatusCode,
		"statusText": http.StatusText(resp.StatusCode),
		"headers":    hdrs,
		"body":       bodyStr,
		"size":       size,
		"durationMs": time.Since(start).Milliseconds(),
		"finalUrl":   resp.Request.URL.String(),
	})
}

// ---- 全局设置 ----

// GetSettings GET /api/settings(管理员)
func (s *Server) GetSettings(c *gin.Context) {
	title, _ := s.Store.GetSetting("platform_title")
	logo, _ := s.Store.GetSetting("platform_logo")
	mockPort, _ := s.Store.GetSetting("mock_port")
	corsOrigins, _ := s.Store.GetSetting("mock_cors_origins")
	ok(c, gin.H{
		"title":          defaultStr(title, "LiteApiDoc"),
		"logo":           logo,
		"mockPort":       defaultStr(mockPort, "9901"),
		"mockCorsOrigins": corsOrigins,
		"dbPath":         s.DBPath,
		"version":        s.Version,
	})
}

// UpdateSettings PUT /api/settings(管理员);端口修改重启后生效
func (s *Server) UpdateSettings(c *gin.Context) {
	var req struct {
		Title           *string `json:"title"`
		Logo            *string `json:"logo"`
		MockPort        *string `json:"mockPort"`
		MockCorsOrigins *string `json:"mockCorsOrigins"`
	}
	if !bindJSON(c, &req) {
		return
	}
	set := func(key string, val *string) error {
		if val == nil {
			return nil
		}
		return s.Store.SetSetting(key, *val)
	}
	if req.Title != nil && strings.TrimSpace(*req.Title) == "" {
		fail(c, http.StatusBadRequest, "平台标题不能为空")
		return
	}
	if req.MockPort != nil {
		p := strings.TrimSpace(*req.MockPort)
		if n := parsePort(p); n < 1 || n > 65535 {
			fail(c, http.StatusBadRequest, "Mock 端口不合法(1~65535)")
			return
		}
	}
	for _, kv := range []struct {
		k string
		v *string
	}{{"platform_title", req.Title}, {"platform_logo", req.Logo},
		{"mock_port", req.MockPort}, {"mock_cors_origins", req.MockCorsOrigins}} {
		if err := set(kv.k, kv.v); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	ok(c, gin.H{"updated": true, "note": "端口与 CORS 白名单修改在重启服务后生效"})
}

func parsePort(s string) int {
	n := 0
	if s == "" {
		return -1
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
		if n > 65535 {
			return -1
		}
	}
	return n
}

func defaultStr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// ---- 管理员操作 ----

// AdminBackup GET /api/admin/backup 下载 SQLite 快照副本
func (s *Server) AdminBackup(c *gin.Context) {
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("liteapidoc-backup-%d.db", time.Now().UnixNano()))
	if err := s.Store.BackupTo(tmp); err != nil {
		fail(c, http.StatusInternalServerError, "备份失败: "+err.Error())
		return
	}
	defer os.Remove(tmp)
	name := fmt.Sprintf("liteapidoc-backup-%s.db", time.Now().Format("20060102-150405"))
	c.FileAttachment(tmp, name)
}

// AdminClear POST /api/admin/clear {confirm:true} 清空全部业务数据(保留平台设置)
func (s *Server) AdminClear(c *gin.Context) {
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if !req.Confirm {
		fail(c, http.StatusBadRequest, "请携带 confirm:true 二次确认")
		return
	}
	if err := s.Store.ClearAllData(); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"cleared": true})
}
