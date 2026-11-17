package server

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
	"xhs-comment-console/internal/browser"
	cc "xhs-comment-console/internal/commentconsole"
	"xhs-comment-console/internal/configs"
	"xhs-comment-console/internal/cookies"
)

// version 构建版本号，通过 -X xhs-comment-console/internal/server.version=vX.Y.Z 注入。
var version = "dev"

// Run 启动独立后端，前端构建产物从指定目录提供。
func Run() {
	var (
		headless   bool
		port       string
		token      string
		demo       bool
		localOCR   bool
		configPath string
		webDir     string
	)
	flag.BoolVar(&headless, "headless", true, "是否无头模式")
	flag.StringVar(&port, "port", ":18060", "端口")
	flag.StringVar(&token, "token", "", "鉴权 Token，留空则读取 AUTH_TOKEN")
	flag.BoolVar(&demo, "demo", false, "控制台演示模式，使用 MySQL，不连接小红书和外部模型")
	flag.BoolVar(&localOCR, "local-ocr", false, "启动或复用本机 RapidOCR")
	flag.StringVar(&configPath, "config", "config/mysql.json", "MySQL 本地配置文件路径")
	flag.StringVar(&webDir, "web-dir", "../frontend/dist", "前端构建目录")
	flag.Parse()
	if token == "" {
		token = os.Getenv("AUTH_TOKEN")
	}
	if os.Getenv("COOKIES_PATH") == "" {
		if err := os.Setenv("COOKIES_PATH", "data/cookies.json"); err != nil {
			logrus.Fatal("设置默认会话路径失败")
		}
	}

	logrus.Infof("comment-console version: %s", version)
	dsn, err := loadConsoleDSN(configPath, demo)
	if err != nil {
		logrus.Fatal(err)
	}

	// 只用内置浏览器。启动时就备好，缺它直接退出，不拖到第一个请求才失败。
	if !demo {
		binPath, err := browser.EnsureBrowser()
		if err != nil {
			logrus.Fatalf("%v", err)
		}
		logrus.Infof("using browser binary: %s", binPath)
	}

	configs.InitHeadless(headless)
	// 入口层解析出 seed 和代理，经 configs 透传给浏览器工厂。
	// seed 取值：环境变量 > 会话文件 > 新生成并写回，保证同一账号每次启动一致。
	if !demo {
		configs.SetFingerprintSeed(configs.ResolveFingerprintSeed(
			cookies.NewLoadCookie(cookies.GetCookiesFilePath())))
	}
	configs.SetProxy(configs.ProxyFromEnv())

	// 初始化服务
	xiaohongshuService := NewXiaohongshuService()

	// 创建并启动应用服务器
	appServer := NewAppServer(xiaohongshuService, token)
	appServer.webDir = webDir
	var backend cc.Backend = consoleBackend{service: xiaohongshuService}
	if demo {
		backend = cc.DemoBackend{}
	}
	engine, err := cc.New(dsn, backend, demo)
	if err != nil {
		logrus.Fatalf("初始化控制台失败: %v", err)
	}
	defer engine.Close()
	if demo {
		if err := engine.SeedDemo(); err != nil {
			logrus.Fatalf("初始化演示数据失败: %v", err)
		}
	}
	appServer.console = engine
	stopOCR := func() {}
	if localOCR && !demo {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		stopOCR, err = startLocalOCR(ctx)
		cancel()
		if err != nil {
			_ = engine.Close()
			logrus.Fatal(err)
		}
	}
	defer stopOCR()
	displayAddress := port
	if strings.HasPrefix(displayAddress, ":") {
		displayAddress = "localhost" + displayAddress
	}
	logrus.Infof("评论管理控制台: http://%s/console/ (v%s)", displayAddress, cc.Version)
	if err := appServer.Start(port); err != nil {
		_ = engine.Close()
		stopOCR()
		logrus.Fatalf("failed to run server: %v", err)
	}
}
