// LiteApiDoc — 轻量化接口文档 + Mock 一体化平台。
// 单进程双端口:Web(API+嵌入式前端)与 Mock 服务。
package main

import (
"context"
"flag"
"fmt"
"io"
"log"
"net/http"
"os"
"os/signal"
"path/filepath"
"syscall"
"time"

	"github.com/gin-gonic/gin"

	"liteapidoc/internal/api"
	"liteapidoc/internal/auth"
	"liteapidoc/internal/mock"
	"liteapidoc/internal/store"
)

const version = "1.1.0"

func main() {
	webPort := flag.Int("web", 9900, "Web 服务端口")
	mockPort := flag.Int("mock", 0, "Mock 服务端口(0=读取平台设置,默认 9901;设置修改需重启生效)")
	dbPath := flag.String("db", "./data/liteapidoc.db", "SQLite 数据文件路径")
	dev := flag.Bool("dev", false, "gin 调试模式")
	showVersion := flag.Bool("version", false, "打印版本号退出")
	flag.Parse()

	if *showVersion {
		fmt.Println("LiteApiDoc", version)
		return
	}
	if !*dev {
		gin.SetMode(gin.ReleaseMode)
	}

	// 日志落文件(同库目录),崩溃时留有现场
	logFile, _ := os.OpenFile(filepath.Join(filepath.Dir(*dbPath), "liteapidoc.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if logFile != nil {
		log.SetOutput(io.MultiWriter(os.Stdout, logFile))
		defer logFile.Close()
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer st.Close()

	// 设置引导:签名密钥、标题
	secret, _ := st.GetSetting("secret")
	if secret == "" {
		secret = auth.GenSecret()
		_ = st.SetSetting("secret", secret)
	}
	if title, _ := st.GetSetting("platform_title"); title == "" {
		_ = st.SetSetting("platform_title", "LiteApiDoc")
	}
	port := *mockPort
	if port == 0 {
		port = parsePortSetting(mustSetting(st, "mock_port"), 9901)
	}

	server := &api.Server{
		Store:    st,
		TM:       auth.NewManager(secret),
		Lockout:  auth.NewLockout(),
		Version:  version,
		MockPort: port,
		DBPath:   *dbPath,
	}

	// Mock 服务(独立端口,同进程)
	mockRouter := gin.New()
	mockRouter.Use(gin.Recovery())
	(&mock.Engine{Store: st}).Register(mockRouter)
	mockSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mockRouter,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("Mock 服务: http://localhost:%d/mock/{项目ID}/…", port)
		if err := mockSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Mock 服务启动失败: %v", err)
		}
	}()

	webSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", *webPort),
		Handler:           api.NewRouter(server),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := webSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Web 服务启动失败: %v", err)
		}
	}()
	log.Printf("LiteApiDoc v%s 就绪  Web: http://localhost:%d  数据: %s", version, *webPort, *dbPath)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("正在关闭…")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = webSrv.Shutdown(ctx)
	_ = mockSrv.Shutdown(ctx)
}

func mustSetting(st *store.Store, key string) string {
	v, _ := st.GetSetting(key)
	return v
}

func parsePortSetting(s string, def int) int {
	n := 0
	if s == "" {
		return def
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
	}
	if n < 1 || n > 65535 {
		return def
	}
	return n
}
