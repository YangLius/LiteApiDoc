// Package webfs 通过 go:embed 将前端构建产物嵌入单二进制。
package webfs

import "embed"

//go:embed all:dist
var Dist embed.FS
