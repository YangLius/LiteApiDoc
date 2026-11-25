// 目录类资源处理器:分组、接口、环境、Mock 日志。
package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"liteapidoc/internal/genexample"
	"liteapidoc/internal/model"
)

// ---- 分组 ----

// ListGroups GET /api/projects/:id/groups
func (s *Server) ListGroups(c *gin.Context) {
	list, err := s.Store.ListGroups(pathID(c, "id"))
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []model.Group{}
	}
	ok(c, list)
}

// CreateGroup POST /api/projects/:id/groups
func (s *Server) CreateGroup(c *gin.Context) {
	projectID := pathID(c, "id")
	if _, allowed := s.checkEdit(c, projectID); !allowed {
		return
	}
	var req struct {
		Name     string `json:"name" binding:"required"`
		ParentID int64  `json:"parentId"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if err := s.validateGroupParent(projectID, req.ParentID, 0); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	g := model.Group{ProjectID: projectID, Name: req.Name, ParentID: req.ParentID}
	if err := s.Store.CreateGroup(&g); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, g)
}

// UpdateGroup PATCH /api/groups/:id
func (s *Server) UpdateGroup(c *gin.Context) {
	g, err := s.Store.GetGroup(pathID(c, "id"))
	if err != nil {
		fail(c, http.StatusNotFound, "分组不存在")
		return
	}
	if _, allowed := s.checkEdit(c, g.ProjectID); !allowed {
		return
	}
	var req struct {
		Name      *string `json:"name"`
		ParentID  *int64  `json:"parentId"`
		SortOrder *int64  `json:"sortOrder"`
	}
	if !bindJSON(c, &req) {
		return
	}
	if req.Name != nil {
		if *req.Name == "" {
			fail(c, http.StatusBadRequest, "分组名称不能为空")
			return
		}
		g.Name = *req.Name
	}
	if req.SortOrder != nil {
		g.SortOrder = *req.SortOrder
	}
	if req.ParentID != nil && *req.ParentID != g.ParentID {
		if err := s.validateGroupParent(g.ProjectID, *req.ParentID, g.ID); err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		g.ParentID = *req.ParentID
	}
	if err := s.Store.UpdateGroup(&g); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, g)
}

// DeleteGroup DELETE /api/groups/:id(组内接口移至未分组,子分组一并删除)
func (s *Server) DeleteGroup(c *gin.Context) {
	g, err := s.Store.GetGroup(pathID(c, "id"))
	if err != nil {
		fail(c, http.StatusNotFound, "分组不存在")
		return
	}
	if _, allowed := s.checkEdit(c, g.ProjectID); !allowed {
		return
	}
	if err := s.Store.DeleteGroup(g.ID); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"deleted": g.ID})
}

// SortGroups POST /api/projects/:id/groups/sort  批量拖拽排序 [{id,parentId,sortOrder}]
func (s *Server) SortGroups(c *gin.Context) {
	projectID := pathID(c, "id")
	if _, allowed := s.checkEdit(c, projectID); !allowed {
		return
	}
	var req []struct {
		ID        int64 `json:"id"`
		ParentID  int64 `json:"parentId"`
		SortOrder int64 `json:"sortOrder"`
	}
	if !bindJSON(c, &req) {
		return
	}
	for _, it := range req {
		g, err := s.Store.GetGroup(it.ID)
		if err != nil || g.ProjectID != projectID {
			fail(c, http.StatusBadRequest, "分组不存在或不属于当前项目")
			return
		}
		if err := s.validateGroupParent(projectID, it.ParentID, g.ID); err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		g.ParentID, g.SortOrder = it.ParentID, it.SortOrder
		if err := s.Store.UpdateGroup(&g); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	ok(c, gin.H{"sorted": len(req)})
}

// validateGroupParent 最多 2 级:父必须是本项目一级分组;一级分组含子分组时不可再降级。
func (s *Server) validateGroupParent(projectID, parentID, selfID int64) error {
	if parentID == 0 {
		return nil
	}
	parent, err := s.Store.GetGroup(parentID)
	if err != nil || parent.ProjectID != projectID {
		return errParent("父分组不存在或不属于当前项目")
	}
	if parent.ParentID != 0 {
		return errParent("最多支持 2 级分组,父分组已是二级分组")
	}
	if selfID == 0 {
		return nil // 新建分组:父为一级分组即合法
	}
	if parent.ID == selfID {
		return errParent("不能将分组移动到自身之下")
	}
	children, err := s.Store.ListGroups(projectID)
	if err != nil {
		return err
	}
	for _, cg := range children {
		if cg.ParentID == selfID {
			return errParent("该分组包含子分组,移动后将超过 2 级,请先处理子分组")
		}
	}
	return nil
}

type parentErr string

func (e parentErr) Error() string { return string(e) }

func errParent(msg string) error { return parentErr(msg) }

// ---- 接口 ----

var validMethods = map[string]bool{"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true}

var validStatus = map[string]bool{
	model.ApiStatusDeveloping: true, model.ApiStatusDone: true, model.ApiStatusDeprecated: true,
}

func sanitizeApi(a *model.Api) error {
	a.Method = strings.ToUpper(strings.TrimSpace(a.Method))
	a.Path = strings.TrimSpace(a.Path)
	if a.Name == "" {
		return errParent("接口名称不能为空")
	}
	if !validMethods[a.Method] {
		return errParent("请求方式仅支持 GET/POST/PUT/DELETE/PATCH")
	}
	if !strings.HasPrefix(a.Path, "/") {
		a.Path = "/" + a.Path
	}
	if strings.ContainsAny(a.Path, " ?#") {
		return errParent("接口路径不能包含空格、? 或 #")
	}
	if a.Status == "" {
		a.Status = model.ApiStatusDeveloping
	}
	if !validStatus[a.Status] {
		return errParent("接口状态不合法")
	}
	if a.Mock.DelayMS < 0 {
		a.Mock.DelayMS = 0
	}
	if a.Mock.DelayMS > 5000 {
		a.Mock.DelayMS = 5000
	}
	if a.Mock.ForceCode < 0 || a.Mock.ForceCode > 599 {
		a.Mock.ForceCode = 0
	}
	if a.Mock.DeprecatedMode == "" {
		a.Mock.DeprecatedMode = model.DeprecatedNormal
	}
	if a.Responses == nil {
		a.Responses = []model.ResponseDef{}
	}
	if a.PathParams == nil {
		a.PathParams = []model.Param{}
	}
	if a.QueryParams == nil {
		a.QueryParams = []model.Param{}
	}
	if a.HeaderParams == nil {
		a.HeaderParams = []model.HeaderParam{}
	}
	return nil
}

// ListApis GET /api/projects/:id/apis
func (s *Server) ListApis(c *gin.Context) {
	list, err := s.Store.ListApis(pathID(c, "id"))
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, list)
}

// GetApi GET /api/apis/:id
func (s *Server) GetApi(c *gin.Context) {
	a, err := s.Store.GetApi(pathID(c, "id"))
	if err != nil {
		fail(c, http.StatusNotFound, "接口不存在")
		return
	}
	ok(c, a)
}

// CreateApi POST /api/projects/:id/apis
func (s *Server) CreateApi(c *gin.Context) {
	projectID := pathID(c, "id")
	if _, allowed := s.checkEdit(c, projectID); !allowed {
		return
	}
	var a model.Api
	if !bindJSON(c, &a) {
		return
	}
	a.ID = 0
	a.ProjectID = projectID
	if err := sanitizeApi(&a); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Store.CreateApi(&a); err != nil {
		if !mapStoreErr(c, err) {
			fail(c, http.StatusInternalServerError, err.Error())
		}
		return
	}
	ok(c, a)
}

// UpdateApi PATCH /api/apis/:id
func (s *Server) UpdateApi(c *gin.Context) {
	old, err := s.Store.GetApi(pathID(c, "id"))
	if err != nil {
		fail(c, http.StatusNotFound, "接口不存在")
		return
	}
	if _, allowed := s.checkEdit(c, old.ProjectID); !allowed {
		return
	}
	var a model.Api
	if !bindJSON(c, &a) {
		return
	}
	a.ID = old.ID
	a.ProjectID = old.ProjectID
	if err := sanitizeApi(&a); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.Store.UpdateApi(&a); err != nil {
		if !mapStoreErr(c, err) {
			fail(c, http.StatusInternalServerError, err.Error())
		}
		return
	}
	ok(c, a)
}

// DeleteApi DELETE /api/apis/:id
func (s *Server) DeleteApi(c *gin.Context) {
	a, err := s.Store.GetApi(pathID(c, "id"))
	if err != nil {
		fail(c, http.StatusNotFound, "接口不存在")
		return
	}
	if _, allowed := s.checkEdit(c, a.ProjectID); !allowed {
		return
	}
	if err := s.Store.DeleteApi(a.ID); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"deleted": a.ID})
}

// SortApis POST /api/projects/:id/apis/sort  批量 [{id,groupId,sortOrder}]
func (s *Server) SortApis(c *gin.Context) {
	projectID := pathID(c, "id")
	if _, allowed := s.checkEdit(c, projectID); !allowed {
		return
	}
	var req []struct {
		ID        int64 `json:"id"`
		GroupID   int64 `json:"groupId"`
		SortOrder int64 `json:"sortOrder"`
	}
	if !bindJSON(c, &req) {
		return
	}
	items := make([]struct {
		ID        int64 `json:"id"`
		GroupID   int64 `json:"groupId"`
		SortOrder int64 `json:"sortOrder"`
	}, 0, len(req))
	for _, it := range req {
		a, err := s.Store.GetApi(it.ID)
		if err != nil || a.ProjectID != projectID {
			fail(c, http.StatusBadRequest, "接口不存在或不属于当前项目")
			return
		}
		items = append(items, struct {
			ID        int64 `json:"id"`
			GroupID   int64 `json:"groupId"`
			SortOrder int64 `json:"sortOrder"`
		}{it.ID, it.GroupID, it.SortOrder})
	}
	if err := s.Store.BatchSortApis(items); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"sorted": len(items)})
}

// GenExample POST /api/gen-example  {schema} → 根据简化 JSON Schema 生成示例
func (s *Server) GenExample(c *gin.Context) {
	var req struct {
		Schema string `json:"schema"`
	}
	if !bindJSON(c, &req) {
		return
	}
	ok(c, gin.H{"example": genexample.FromSchemaString(req.Schema)})
}

// ---- 环境 ----

// ListEnvs GET /api/projects/:id/envs
func (s *Server) ListEnvs(c *gin.Context) {
	list, err := s.Store.ListEnvs(pathID(c, "id"))
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []model.Env{}
	}
	ok(c, list)
}

// CreateEnv POST /api/projects/:id/envs
func (s *Server) CreateEnv(c *gin.Context) {
	projectID := pathID(c, "id")
	if _, allowed := s.checkEdit(c, projectID); !allowed {
		return
	}
	var e model.Env
	if !bindJSON(c, &e) {
		return
	}
	e.ID = 0
	e.ProjectID = projectID
	if e.Name == "" {
		fail(c, http.StatusBadRequest, "环境名称不能为空")
		return
	}
	if err := s.Store.CreateEnv(&e); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, e)
}

// UpdateEnv PATCH /api/envs/:id
func (s *Server) UpdateEnv(c *gin.Context) {
	e, err := s.Store.GetEnv(pathID(c, "id"))
	if err != nil {
		fail(c, http.StatusNotFound, "环境不存在")
		return
	}
	if _, allowed := s.checkEdit(c, e.ProjectID); !allowed {
		return
	}
	var req model.Env
	if !bindJSON(c, &req) {
		return
	}
	e.Name, e.BaseURL, e.SortOrder = req.Name, req.BaseURL, req.SortOrder
	if e.Name == "" {
		fail(c, http.StatusBadRequest, "环境名称不能为空")
		return
	}
	if err := s.Store.UpdateEnv(&e); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, e)
}

// DeleteEnv DELETE /api/envs/:id
func (s *Server) DeleteEnv(c *gin.Context) {
	e, err := s.Store.GetEnv(pathID(c, "id"))
	if err != nil {
		fail(c, http.StatusNotFound, "环境不存在")
		return
	}
	if _, allowed := s.checkEdit(c, e.ProjectID); !allowed {
		return
	}
	if err := s.Store.DeleteEnv(e.ID); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"deleted": e.ID})
}

// ---- Mock 日志 ----

// ListMockLogs GET /api/projects/:id/mock-logs
func (s *Server) ListMockLogs(c *gin.Context) {
	list, err := s.Store.ListMockLogs(pathID(c, "id"), 100)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []model.MockLog{}
	}
	ok(c, list)
}

// ClearMockLogs DELETE /api/projects/:id/mock-logs
func (s *Server) ClearMockLogs(c *gin.Context) {
	projectID := pathID(c, "id")
	if _, allowed := s.checkEdit(c, projectID); !allowed {
		return
	}
	if err := s.Store.ClearMockLogs(projectID); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"cleared": true})
}
