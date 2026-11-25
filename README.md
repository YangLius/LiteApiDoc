# LiteApiDoc

轻量化接口文档 + Mock 一体化平台(PRD V1.1 实现)。「文档即 Mock」:录入接口自动生成文档与 Mock 服务,支持在线调试、OpenAPI3.0 导入导出、只读分享、单文件 SQLite 存储。

## 特性(MVP V1.0/V1.1)

- **文档即 Mock**:录入响应示例即为 Mock 返回;精确 + 路径模板路由匹配(`{id}` 占位),固定段多者优先
- **Mock 能力**:响应延时 0~5000ms、强制响应码(优先同码场景示例)、自定义返回、项目级总开关(关闭统一 503)、废弃接口 404/提示模式、请求日志(项目级 100 条)
- **双密码模型**:平台管理密码(管理操作)+ 项目访问密码(区分开发者/访客),免注册登录;防爆破锁定
- **在线调试**:后端代理转发(规避 CORS/自签证书),多环境切换,自动带入接口参数
- **导入导出**:OpenAPI3.0 yaml/json(basePath 拼接、批量冲突策略)、**Swagger 2.0 自动转换**($ref 自动内联)、**服务地址导入**(自动探测 swagger/doc.json、v3/api-docs、openapi.json 等常见端点,可保存为同步源一键同步)、项目 JSON 备份、离线 HTML 文档
- **分享**:只读分享链接,可设密码、随时失效
- **部署**:Go 单二进制(纯 Go SQLite,无 CGO)/ Docker 一键启动;前端嵌入二进制

## 快速开始

### 方式一:单二进制

```bash
./build.sh          # 构建前端 + 编译单二进制 → dist/liteapidoc
./dist/liteapidoc -web 9900 -mock 9901 -db ./data/liteapidoc.db
```

浏览器打开 `http://localhost:9900`。首次使用需设置**平台管理密码**。

### 方式二:Docker

```bash
docker build -t liteapidoc .
docker run -d -p 9900:9900 -p 9901:9901 -v ./data:/data liteapidoc
```

### 前端开发模式

```bash
cd web && npm run dev   # Vite 代理 /api→9900、/mock→9901
cd backend && go run . -dev
```

## 命令行参数

| 参数 | 默认 | 说明 |
| ---- | ---- | ---- |
| `-web` | 9900 | Web 服务端口 |
| `-mock` | 0 | Mock 端口(0=读取平台设置,默认 9901) |
| `-db` | ./data/liteapidoc.db | SQLite 数据文件路径 |
| `-dev` | false | Gin 调试模式 |
| `-version` | - | 打印版本号 |

## 端口与安全(重要)

- **Mock 端点不做鉴权**(PRD V1.1 明确取舍):内网任何人可调用;请勿将 Mock 端口暴露到公网
- 需收紧跨域时,在「平台设置 → Mock CORS 白名单」配置允许的前端来源
- 数据仅存本机 SQLite;请使用「平台设置 → 下载 SQLite 备份」定期备份

## 目录结构

```text
backend/            Go 后端(Gin + modernc.org/sqlite 纯Go驱动)
  internal/store    SQLite 访问与迁移
  internal/api      Web API(路由/中间件/处理器)
  internal/mock     Mock 引擎(路由匹配/CORS/日志)
  internal/openapi  OpenAPI3.0 导入导出
  internal/auth     双密码模型(bcrypt + HMAC token + 防爆破)
  internal/genexample  JSON Schema → 示例生成
  internal/webfs    go:embed 前端产物
web/                Vue3 + Element Plus 前端(Vite)
Dockerfile          多阶段构建
build.sh            一键构建脚本
```
