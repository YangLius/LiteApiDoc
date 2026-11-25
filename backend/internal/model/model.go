// Package model 定义 LiteApiDoc 全部数据模型与 JSON 存储辅助结构。
package model

import "encoding/json"

const (
	// ApiStatus 接口状态
	ApiStatusDeveloping = "developing" // 开发中
	ApiStatusDone       = "done"       // 已完成
	ApiStatusDeprecated = "deprecated" // 已废弃

	// HTTP methods supported in V1
	Methods = "GET,POST,PUT,DELETE,PATCH"

	// DeprecatedMode 废弃接口的 Mock 行为
	DeprecatedNormal = "normal" // 照常 Mock
	Deprecated404    = "404"    // 返回 404
	DeprecatedTip    = "tip"    // 返回废弃提示 JSON
)

// KV 通用键值对(全局请求 Header / 全局 Mock 返回 Header)。
type KV struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Remark string `json:"remark"`
}

// Param 通用参数定义(Path/Query)。
type Param struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // string/integer/number/boolean/array
	Required bool   `json:"required"`
	Desc     string `json:"desc"`
	Example  string `json:"example"`
}

// HeaderParam Header 参数。
type HeaderParam struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Remark  string `json:"remark"`
	Default bool   `json:"default"` // 调试时是否默认携带
}

// ResponseDef 响应场景定义。
type ResponseDef struct {
	Code    int             `json:"code"`
	Desc    string          `json:"desc"`
	Schema  json.RawMessage `json:"schema,omitempty"`  // JSON Schema
	Example string          `json:"example,omitempty"` // 示例返回(原始 JSON 字符串)
}

// MockConfig 单接口 Mock 策略。
type MockConfig struct {
	DelayMS         int    `json:"delayMs"`         // 0~5000
	ForceCode       int    `json:"forceCode"`       // 0=默认按文档;>0 强制返回该状态码
	CustomJSON      string `json:"customJson"`      // 非空时(默认策略下)优先返回
	DeprecatedMode  string `json:"deprecatedMode"`  // normal|404|tip
}

// Project 项目。
type Project struct {
	ID                int64           `json:"id"`
	Name              string          `json:"name"`
	Description       string          `json:"description"`
	Tags              []string        `json:"tags"`
	HasPassword       bool            `json:"hasPassword"`
	Archived          bool            `json:"archived"`
	MockEnabled       bool            `json:"mockEnabled"`
	GlobalHeaders     []KV            `json:"globalHeaders"`     // 请求 Header 复用(文档/调试)
	GlobalMockHeaders []KV            `json:"globalMockHeaders"` // Mock 返回 Header
	SortOrder         int64           `json:"sortOrder"`
	CreatedAt         int64           `json:"createdAt"`
	UpdatedAt         int64           `json:"updatedAt"`
	ApiCount          int64           `json:"apiCount"` // 列表查询附加统计
}

// Group 接口分组,最多 2 级(ParentID=0 为一级)。
type Group struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"projectId"`
	Name      string `json:"name"`
	ParentID  int64  `json:"parentId"` // 0=一级分组
	SortOrder int64  `json:"sortOrder"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// Api 接口文档。
type Api struct {
	ID           int64           `json:"id"`
	ProjectID    int64           `json:"projectId"`
	GroupID      int64           `json:"groupId"` // 0=未分组
	Name         string          `json:"name"`
	Method       string          `json:"method"`
	Path         string          `json:"path"` // 如 /users/{id},以 / 开头
	Status       string          `json:"status"`
	Owner        string          `json:"owner"`
	Remark       string          `json:"remark"`
	PathParams   []Param         `json:"pathParams"`
	QueryParams  []Param         `json:"queryParams"`
	HeaderParams []HeaderParam   `json:"headerParams"`
	BodySchema   json.RawMessage `json:"bodySchema,omitempty"` // JSON Schema(application/json)
	BodyExample  string          `json:"bodyExample,omitempty"`
	Responses    []ResponseDef   `json:"responses"`
	Mock         MockConfig      `json:"mock"`
	SortOrder    int64           `json:"sortOrder"`
	CreatedAt    int64           `json:"createdAt"`
	UpdatedAt    int64           `json:"updatedAt"`
}

// Env 调试环境。
type Env struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"projectId"`
	Name      string `json:"name"`
	BaseURL   string `json:"baseUrl"`
	SortOrder int64  `json:"sortOrder"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// MockLog Mock 请求日志(项目级保留最近 100 条)。
type MockLog struct {
	ID         int64  `json:"id"`
	ProjectID  int64  `json:"projectId"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Query      string `json:"query"`
	RespCode   int    `json:"respCode"`
	DurationMS int64  `json:"durationMs"`
	CreatedAt  int64  `json:"createdAt"`
}

// Share 分享链接配置。
type Share struct {
	ID           int64  `json:"id"`
	ProjectID    int64  `json:"projectId"`
	Token        string `json:"token"`
	HasPassword  bool   `json:"hasPassword"`
	Enabled      bool   `json:"enabled"`
	CreatedAt    int64  `json:"createdAt"`
}

// TreeItem 项目详情侧边树节点(分组或接口)。
type TreeItem struct {
	Type     string  `json:"type"` // group | api
	ID       int64   `json:"id"`
	ParentID int64   `json:"parentId"`
	Name     string  `json:"name"`
	Method   string  `json:"method,omitempty"`
	Path     string  `json:"path,omitempty"`
	Status   string  `json:"status,omitempty"`
	Sort     int64   `json:"sort"`
	Children []TreeItem `json:"children,omitempty"`
}
