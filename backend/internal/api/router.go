package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"liteapidoc/internal/webfs"
)

// NewRouter 组装全部路由(含嵌入式前端静态资源与 SPA fallback)。
func NewRouter(s *Server) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	g := r.Group("/api")
	{
		g.GET("/meta", s.Meta)

		g.POST("/auth/admin/setup", s.AdminSetup)
		g.POST("/auth/admin/login", s.AdminLogin)
		g.POST("/gen-example", s.GenExample)

		g.GET("/projects", s.ListProjects)
		g.POST("/projects", s.RequireAdmin(), s.CreateProject)
		g.GET("/projects/:id", s.GetProject)
		g.PATCH("/projects/:id", s.UpdateProject)
		g.DELETE("/projects/:id", s.RequireAdmin(), s.DeleteProject)
		g.POST("/projects/:id/archive", s.ArchiveProject)
		g.POST("/projects/:id/unlock", s.ProjectUnlock)
		g.PUT("/projects/:id/password", s.SetProjectPassword)

		g.GET("/projects/:id/groups", s.ListGroups)
		g.POST("/projects/:id/groups", s.CreateGroup)
		g.POST("/projects/:id/groups/sort", s.SortGroups)
		g.PATCH("/groups/:id", s.UpdateGroup)
		g.DELETE("/groups/:id", s.DeleteGroup)

		g.GET("/projects/:id/apis", s.ListApis)
		g.POST("/projects/:id/apis", s.CreateApi)
		g.POST("/projects/:id/apis/sort", s.SortApis)
		g.GET("/apis/:id", s.GetApi)
		g.PATCH("/apis/:id", s.UpdateApi)
		g.DELETE("/apis/:id", s.DeleteApi)

		g.GET("/projects/:id/envs", s.ListEnvs)
		g.POST("/projects/:id/envs", s.CreateEnv)
		g.PATCH("/envs/:id", s.UpdateEnv)
		g.DELETE("/envs/:id", s.DeleteEnv)

		g.GET("/projects/:id/mock-logs", s.ListMockLogs)
		g.DELETE("/projects/:id/mock-logs", s.ClearMockLogs)

		g.GET("/projects/:id/share", s.ShareGet)
		g.PUT("/projects/:id/share", s.ShareUpsert)
		g.DELETE("/projects/:id/share", s.ShareDelete)
		g.GET("/share/:token", s.ShareInfo)
		g.POST("/share/:token/unlock", s.ShareUnlock)
		g.GET("/share/:token/doc", s.ShareDoc)

		g.POST("/debug", s.DebugProxy)

		adm := g.Group("", s.RequireAdmin())
		adm.GET("/settings", s.GetSettings)
		adm.PUT("/settings", s.UpdateSettings)
		adm.GET("/admin/backup", s.AdminBackup)
		adm.POST("/admin/clear", s.AdminClear)
		adm.POST("/admin/import/project", s.ImportProjectJSON)

		g.POST("/projects/:id/import/openapi/analyze", s.OpenAPIAnalyze)
		g.POST("/projects/:id/import/openapi/apply", s.OpenAPIApply)
		g.POST("/projects/:id/import/spec-url/analyze", s.SpecURLAnalyze)
		g.GET("/projects/:id/export/openapi", s.ExportOpenAPI)
		g.GET("/projects/:id/export/json", s.ExportJSON)
		g.GET("/projects/:id/export/html", s.ExportHTML)
	}

	// 前端静态资源:真实文件优先,其余路径回退 index.html(SPA)
	// index.html 必须 no-cache:SPA 入口引用带内容哈希的 chunk,缓存旧入口会导致
	// 新版本发布后浏览器仍加载旧代码;assets/* 文件名含哈希,可长缓存。
	if dist, err := fs.Sub(webfs.Dist, "dist"); err == nil {
		fileServer := http.FileServer(http.FS(dist))
		r.NoRoute(func(c *gin.Context) {
			p := c.Request.URL.Path
			if strings.HasPrefix(p, "/api") || strings.HasPrefix(p, "/mock/") || p == "/mock" {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "not found"})
				return
			}
			if strings.HasPrefix(p, "/assets/") {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
				if f, err := dist.Open(strings.TrimPrefix(p, "/")); err == nil {
					_ = f.Close()
					fileServer.ServeHTTP(c.Writer, c.Request)
					return
				}
				c.Header("Cache-Control", "no-cache")
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "asset not found"})
				return
			}
			index, err2 := fs.ReadFile(dist, "index.html")
			if err2 != nil {
				c.String(http.StatusNotFound, "frontend not built")
				return
			}
			c.Header("Cache-Control", "no-cache")
			c.Data(http.StatusOK, "text/html; charset=utf-8", index)
		})
	}
	return r
}
