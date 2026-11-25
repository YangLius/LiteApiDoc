package openapi

// Normalize/Parse 转换管线单元测试:Swagger 2.0 → OpenAPI 3 转换、$ref 内联、解析产物。

import (
	"strings"
	"testing"
)

const swagger2Sample = `
swagger: "2.0"
info:
  title: 宠物商店 API
  version: "1.0"
host: petstore.example.com
basePath: /v2
schemes: [https]
paths:
  /pets/{petId}:
    get:
      summary: 查询宠物详情
      parameters:
        - name: petId
          in: path
          required: true
          type: integer
          description: 宠物ID
        - name: full
          in: query
          type: boolean
      responses:
        200:
          description: OK
          schema:
            $ref: '#/definitions/Pet'
          examples:
            application/json: {"id": 1, "name": "旺财"}
  /pets:
    post:
      summary: 新增宠物
      parameters:
        - name: body
          in: body
          required: true
          schema:
            $ref: '#/definitions/Pet'
        - name: token
          in: formData
          type: string
      responses:
        201:
          description: 创建成功
definitions:
  Pet:
    type: object
    required: [name]
    properties:
      id:
        type: integer
      name:
        type: string
        description: 宠物名字
      tag:
        $ref: '#/definitions/Tag'
  Tag:
    type: object
    properties:
      label:
        type: string
`

func TestNormalizeSwagger2(t *testing.T) {
	out, kind, warns, err := Normalize(swagger2Sample)
	if err != nil {
		t.Fatalf("Normalize 失败: %v", err)
	}
	if kind != "swagger2" {
		t.Fatalf("kind = %q, 期望 swagger2", kind)
	}
	if len(warns) == 0 || !containsAny(warns, "formData") {
		t.Fatalf("应包含 formData 跳过警告, 实际: %v", warns)
	}
	// $ref 已内联:Tag.label 应直接出现在 200 响应 schema 中,不再有 $ref
	if strings.Contains(out, `"#/definitions`) {
		t.Fatalf("definitions 引用未内联: %s", truncateStr(out, 200))
	}
	if !strings.Contains(out, `"label"`) {
		t.Fatal("嵌套引用 Tag.label 未内联")
	}
	// servers 由 host+scheme+basePath 拼接
	if !strings.Contains(out, `https://petstore.example.com/v2`) {
		t.Fatalf("servers 拼接错误: %s", truncateStr(out, 200))
	}

	base, apis, title, err := Parse(out)
	if err != nil {
		t.Fatalf("Parse 归一化内容失败: %v", err)
	}
	if title != "宠物商店 API" || base != "/v2" {
		t.Fatalf("title=%q basePath=%q", title, base)
	}
	if len(apis) != 2 {
		t.Fatalf("应解析出 2 个接口, 实际 %d", len(apis))
	}
	// find GET /pets/{petId}
	var detail, create *ParsedApi
	for i := range apis {
		if apis[i].Method == "GET" {
			detail = &apis[i]
		}
		if apis[i].Method == "POST" {
			create = &apis[i]
		}
	}
	if detail == nil || create == nil {
		t.Fatalf("接口缺失: %+v", apis)
	}
	if detail.Path != "/pets/{petId}" || len(detail.PathParams) != 1 ||
		detail.PathParams[0].Name != "petId" || detail.PathParams[0].Type != "integer" {
		t.Fatalf("path 参数解析错误: %+v", detail.PathParams)
	}
	if len(detail.QueryParams) != 1 || detail.QueryParams[0].Name != "full" {
		t.Fatalf("query 参数解析错误: %+v", detail.QueryParams)
	}
	if len(detail.Responses) != 1 || detail.Responses[0].Code != 200 {
		t.Fatalf("响应解析错误: %+v", detail.Responses)
	}
	if !strings.Contains(string(detail.Responses[0].Schema), `"label"`) ||
		!strings.Contains(detail.Responses[0].Example, "旺财") {
		t.Fatalf("响应 schema/example 内联错误: %s | %s", detail.Responses[0].Schema, detail.Responses[0].Example)
	}
	if create.BodySchema == "" || !strings.Contains(create.BodySchema, `"name"`) {
		t.Fatalf("body 参数 → requestBody 转换失败: %s", create.BodySchema)
	}
}

const openapi3Sample = `
openapi: 3.0.1
info: {title: 用户服务, version: "1"}
servers: [{url: "https://api.x.com/v1"}]
paths:
  /u/{id}:
    get:
      summary: 详情
      parameters: [{name: id, in: path, required: true, schema: {type: string}}]
      responses:
        200:
          description: ok
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/User'
components:
  schemas:
    User:
      type: object
      properties:
        name: {type: string}
        addr:
          $ref: '#/components/schemas/Addr'
    Addr:
      type: object
      properties:
        city: {type: string}
`

func TestNormalizeOpenAPI3InlineRefs(t *testing.T) {
	out, kind, warns, err := Normalize(openapi3Sample)
	if err != nil {
		t.Fatalf("Normalize 失败: %v", err)
	}
	if kind != "openapi3" {
		t.Fatalf("kind = %q", kind)
	}
	if len(warns) != 0 {
		t.Fatalf("不应有警告: %v", warns)
	}
	if strings.Contains(out, `"$ref"`) {
		t.Fatalf("$ref 未全部内联: %s", truncateStr(out, 200))
	}
	if !strings.Contains(out, `"city"`) {
		t.Fatal("嵌套 $ref(Addr.city)未内联")
	}
	_, apis, _, err := Parse(out)
	if err != nil || len(apis) != 1 || apis[0].Path != "/u/{id}" {
		t.Fatalf("Parse 失败: %v, apis=%d", err, len(apis))
	}
}

func TestNormalizeRejectsUnknown(t *testing.T) {
	if _, _, _, err := Normalize(`{"openapi": "4.0.0"}`); err == nil {
		t.Fatal("OpenAPI 4.0 应被拒绝")
	}
	if _, _, _, err := Normalize(`{"random": true}`); err == nil {
		t.Fatal("非规范内容应被拒绝")
	}
}

func containsAny(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
