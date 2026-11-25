// Package api 实现平台 Web API(路由、中间件、处理器)。
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"liteapidoc/internal/auth"
	"liteapidoc/internal/model"
	"liteapidoc/internal/store"
)

// Server API 服务上下文。
type Server struct {
	Store   *store.Store
	TM      *auth.Manager
	Lockout *auth.Lockout
	Version string
	MockPort int // 当前生效的 Mock 端口(展示用)
	DBPath  string
}

// ---- 响应约定 ----

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok", "data": data})
}

func fail(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"code": status, "msg": msg})
}

// ---- 通用工具 ----

func tokenFrom(c *gin.Context) string { return c.GetHeader("X-Auth-Token") }

func pathID(c *gin.Context, name string) int64 {
	id, _ := strconv.ParseInt(c.Param(name), 10, 64)
	return id
}

func bindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		fail(c, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return false
	}
	return true
}

// isAdmin 当前请求是否携带有效平台管理 token。
func (s *Server) isAdmin(c *gin.Context) bool {
	if tokenFrom(c) == "" {
		return false
	}
	_, err := s.TM.Verify(tokenFrom(c), auth.KindAdmin)
	return err == nil
}

// checkEdit 校验对项目的编辑权限(双密码模型):
//  1. 有效平台管理 token → 放行(管理员可重置/管理任意项目);
//  2. 项目已开启访问密码 → 需有效项目编辑 token;
//  3. 未开启密码 → 内网信任模式,放行。
// 归档项目只读。失败时已写响应并返回 false。
func (s *Server) checkEdit(c *gin.Context, projectID int64) (model.Project, bool) {
	p, err := s.Store.GetProject(projectID)
	if err != nil {
		fail(c, http.StatusNotFound, "项目不存在")
		return p, false
	}
	if s.isAdmin(c) {
		return p, true
	}
	if p.Archived {
		fail(c, http.StatusLocked, "项目已归档,处于只读状态")
		return p, false
	}
	if p.HasPassword {
		if tokenFrom(c) == "" {
			fail(c, http.StatusUnauthorized, "该项目已开启访问密码,请先解锁后编辑")
			return p, false
		}
		if id, err := s.TM.Verify(tokenFrom(c), auth.KindProj); err != nil || id != p.ID {
			fail(c, http.StatusUnauthorized, "访问密码凭证无效或已过期,请重新解锁")
			return p, false
		}
	}
	return p, true
}

// RequireAdmin 平台级管理操作中间件。
func (s *Server) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !s.isAdmin(c) {
			fail(c, http.StatusUnauthorized, "需要平台管理员权限,请先输入平台管理密码")
			c.Abort()
			return
		}
		c.Next()
	}
}

// mapStoreErr 将存储层错误映射为响应(接口唯一性/模板冲突)。
func mapStoreErr(c *gin.Context, err error) bool {
	switch {
	case errors.Is(err, store.ErrConflict):
		fail(c, http.StatusConflict, "保存失败:同请求方式+接口路径已存在")
		return true
	case errors.Is(err, store.ErrTemplateConflict):
		fail(c, http.StatusConflict, "保存失败:Mock路由模板冲突(固定段与占位位置一致,无法消歧),请调整路径")
		return true
	case errors.Is(err, store.ErrNotFound):
		fail(c, http.StatusNotFound, "资源不存在")
		return true
	}
	return false
}

// ---- 元信息 ----

// Meta 公开元信息(分享页/前端引导也需要)。
func (s *Server) Meta(c *gin.Context) {
	title, _ := s.Store.GetSetting("platform_title")
	if title == "" {
		title = "LiteApiDoc"
	}
	logo, _ := s.Store.GetSetting("platform_logo")
	adminHash, _ := s.Store.GetSetting("admin_pwd_hash")
	ok(c, gin.H{
		"version":          s.Version,
		"title":            title,
		"logo":             logo,
		"adminInitialized": adminHash != "",
		"mockPort":         s.MockPort,
		"dbPath":           s.DBPath,
	})
}

// ---- 认证 ----

// AdminSetup 首次启动设置平台管理密码(仅未初始化时可用)。
func (s *Server) AdminSetup(c *gin.Context) {
	var req struct {
		Password string `json:"password" binding:"required,min=4"`
	}
	if !bindJSON(c, &req) {
		return
	}
	existing, _ := s.Store.GetSetting("admin_pwd_hash")
	if existing != "" {
		fail(c, http.StatusConflict, "平台管理密码已设置")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.Store.SetSetting("admin_pwd_hash", hash); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"token": s.TM.Sign(auth.KindAdmin, 0)})
}

// AdminLogin 校验平台管理密码,签发管理 token。
func (s *Server) AdminLogin(c *gin.Context) {
	const lockKey = "admin"
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
	hash, _ := s.Store.GetSetting("admin_pwd_hash")
	if hash == "" {
		fail(c, http.StatusPreconditionFailed, "平台管理密码尚未设置,请先初始化")
		return
	}
	if !auth.CheckPassword(hash, req.Password) {
		s.Lockout.Fail(lockKey)
		fail(c, http.StatusUnauthorized, "平台管理密码错误")
		return
	}
	s.Lockout.Reset(lockKey)
	ok(c, gin.H{"token": s.TM.Sign(auth.KindAdmin, 0)})
}

// ProjectUnlock 校验项目访问密码,签发该项目编辑 token。
func (s *Server) ProjectUnlock(c *gin.Context) {
	id := pathID(c, "id")
	lockKey := fmt.Sprintf("proj:%d", id)
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
	hash, err := s.Store.GetProjectPasswordHash(id)
	if errors.Is(err, store.ErrNotFound) {
		fail(c, http.StatusNotFound, "项目不存在")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if hash == "" {
		fail(c, http.StatusBadRequest, "该项目未开启访问密码")
		return
	}
	if !auth.CheckPassword(hash, req.Password) {
		s.Lockout.Fail(lockKey)
		fail(c, http.StatusUnauthorized, "项目访问密码错误")
		return
	}
	s.Lockout.Reset(lockKey)
	ok(c, gin.H{"token": s.TM.Sign(auth.KindProj, id)})
}

// ---- 项目 ----

// ListProjects GET /api/projects
func (s *Server) ListProjects(c *gin.Context) {
	list, err := s.Store.ListProjects()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, list)
}

// CreateProject POST /api/projects(需管理员 token)
func (s *Server) CreateProject(c *gin.Context) {
	var req struct {
		Name        string   `json:"name" binding:"required"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
		Password    string   `json:"password"` // 可选:初始项目访问密码
		MockEnabled *bool    `json:"mockEnabled"`
	}
	if !bindJSON(c, &req) {
		return
	}
	hash := ""
	if req.Password != "" {
		var err error
		if hash, err = auth.HashPassword(req.Password); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	mockEnabled := true
	if req.MockEnabled != nil {
		mockEnabled = *req.MockEnabled
	}
	p := model.Project{
		Name: req.Name, Description: req.Description, Tags: req.Tags, MockEnabled: mockEnabled,
	}
	if err := s.Store.CreateProject(&p, hash); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, p)
}

// GetProject GET /api/projects/:id
func (s *Server) GetProject(c *gin.Context) {
	p, err := s.Store.GetProject(pathID(c, "id"))
	if err != nil {
		fail(c, http.StatusNotFound, "项目不存在")
		return
	}
	ok(c, p)
}

func decodeProjectPatch(raw []byte) (patch func(*model.Project), err error) {
	var req struct {
		Name              *string     `json:"name"`
		Description       *string     `json:"description"`
		Tags              *[]string   `json:"tags"`
		MockEnabled       *bool       `json:"mockEnabled"`
		GlobalHeaders     *[]model.KV `json:"globalHeaders"`
		GlobalMockHeaders *[]model.KV `json:"globalMockHeaders"`
		SpecUrl           *string     `json:"specUrl"`
		SortOrder         *int64      `json:"sortOrder"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	return func(p *model.Project) {
		if req.Name != nil {
			p.Name = *req.Name
		}
		if req.Description != nil {
			p.Description = *req.Description
		}
		if req.Tags != nil {
			p.Tags = *req.Tags
		}
		if req.MockEnabled != nil {
			p.MockEnabled = *req.MockEnabled
		}
		if req.GlobalHeaders != nil {
			p.GlobalHeaders = *req.GlobalHeaders
		}
		if req.GlobalMockHeaders != nil {
			p.GlobalMockHeaders = *req.GlobalMockHeaders
		}
		if req.SpecUrl != nil {
			p.SpecUrl = strings.TrimSpace(*req.SpecUrl)
		}
		if req.SortOrder != nil {
			p.SortOrder = *req.SortOrder
		}
	}, nil
}

// UpdateProject PATCH /api/projects/:id(编辑权)
func (s *Server) UpdateProject(c *gin.Context) {
	p, allowed := s.checkEdit(c, pathID(c, "id"))
	if !allowed {
		return
	}
	raw, err := c.GetRawData()
	if err != nil {
		fail(c, http.StatusBadRequest, "读取请求体失败: "+err.Error())
		return
	}
	patch, err := decodeProjectPatch(raw)
	if err != nil {
		fail(c, http.StatusBadRequest, "请求体解析失败: "+err.Error())
		return
	}
	patch(&p)
	if p.Name == "" {
		fail(c, http.StatusBadRequest, "项目名称不能为空")
		return
	}
	if err := s.Store.UpdateProject(&p); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	fresh, _ := s.Store.GetProject(p.ID)
	ok(c, fresh)
}

// ArchiveProject POST /api/projects/:id/archive {archived}
func (s *Server) ArchiveProject(c *gin.Context) {
	p, allowed := s.checkEdit(c, pathID(c, "id"))
	if !allowed {
		return
	}
	var req struct {
		Archived bool `json:"archived"`
	}
	if !bindJSON(c, &req) {
		return
	}
	p.Archived = req.Archived
	if err := s.Store.UpdateProject(&p); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, p)
}

// SetProjectPassword PUT /api/projects/:id/password {password}
// 密码为空串=清除;有密码项目需编辑 token(或管理员 token)——由 checkEdit 保证。
func (s *Server) SetProjectPassword(c *gin.Context) {
	p, allowed := s.checkEdit(c, pathID(c, "id"))
	if !allowed {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !bindJSON(c, &req) {
		return
	}
	hash := ""
	if req.Password != "" {
		var err error
		if hash, err = auth.HashPassword(req.Password); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := s.Store.SetProjectPassword(p.ID, hash); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"hasPassword": hash != ""})
}

// DeleteProject DELETE /api/projects/:id(需管理员 token)
func (s *Server) DeleteProject(c *gin.Context) {
	id := pathID(c, "id")
	if _, err := s.Store.GetProject(id); err != nil {
		fail(c, http.StatusNotFound, "项目不存在")
		return
	}
	if err := s.Store.DeleteProject(id); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"deleted": id})
}
