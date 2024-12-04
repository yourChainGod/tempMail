package main

import (
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/alash3al/go-smtpsrv/v3"
	"github.com/gin-gonic/gin"
	_ "github.com/joho/godotenv/autoload"
)

// Config 应用配置
type Config struct {
	AllowedDomains []string
	SMTPPort       string
	HTTPPort       string
	HTTPSPort      string
	CertFile       string
	KeyFile        string
	EnableHTTPS    bool
}

// MailContent 邮件内容结构
type mailContent struct {
	from        string
	to          string
	title       string
	TextContent string
	HtmlContent string
	receivedAt  time.Time
}

var (
	mailBox = make(map[string][]mailContent)
	mu      sync.RWMutex
	config  Config
)

// 初始化配置
func initConfig() Config {
	cfg := Config{
		AllowedDomains: strings.Split(os.Getenv("ALLOWED_DOMAINS"), ","),
		SMTPPort:       getEnvOrDefault("SMTP_PORT", "25"),
		HTTPPort:       getEnvOrDefault("HTTP_PORT", "80"),
		HTTPSPort:      getEnvOrDefault("HTTPS_PORT", "443"),
		CertFile:       getEnvOrDefault("CERT_FILE", "./certs/server.pem"),
		KeyFile:        getEnvOrDefault("KEY_FILE", "./certs/server.key"),
		EnableHTTPS:    os.Getenv("ENABLE_HTTPS") == "true",
	}

	if len(cfg.AllowedDomains) == 0 || cfg.AllowedDomains[0] == "" {
		log.Fatal("错误：ALLOWED_DOMAINS 环境变量未设置")
	}

	return cfg
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func handler(c *smtpsrv.Context) error {
	to := strings.Trim(c.To().String(), "<>")
	from := strings.Trim(c.From().String(), "<>")
	msg, err := c.Parse()
	if err != nil {
		log.Printf("解析邮件失败: %v", err)
		return err
	}

	content := mailContent{
		from:        from,
		to:          to,
		title:       msg.Subject,
		TextContent: msg.TextBody,
		HtmlContent: msg.HTMLBody,
		receivedAt:  time.Now(),
	}

	mu.Lock()
	defer mu.Unlock()

	if _, ok := mailBox[to]; !ok {
		mailBox[to] = make([]mailContent, 0, 10)
	}
	mailBox[to] = append(mailBox[to], content)

	log.Printf("收到来自 %s 发送给 %s 的邮件", from, to)
	return nil
}

func startSMTPServer() error {
	cfg := smtpsrv.ServerConfig{
		BannerDomain:    config.AllowedDomains[0],
		ListenAddr:      ":" + config.SMTPPort,
		MaxMessageBytes: 1024 * 1024,
		Handler:         handler,
	}

	log.Printf("SMTP服务器正在启动于端口 %s...", config.SMTPPort)
	return smtpsrv.ListenAndServe(&cfg)
}

func startHTTPServer() {
	gin.SetMode(gin.ReleaseMode)
	httpSrv := gin.Default()

	// 添加恢复中间件
	httpSrv.Use(gin.Recovery())

	// 添加简单的访问日志
	httpSrv.Use(func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		c.Next()
		log.Printf("[%s] %s %s %v", c.Request.Method, path, c.ClientIP(), time.Since(start))
	})

	setupRoutes(httpSrv)

	// 启动 HTTP 服务器
	go func() {
		log.Printf("HTTP服务器正在启动于端口 %s...", config.HTTPPort)
		if err := httpSrv.Run(":" + config.HTTPPort); err != nil {
			log.Printf("HTTP服务器启动失败: %v", err)
		}
	}()

	// 根据配置决定是否启动 HTTPS 服务器
	if config.EnableHTTPS {
		log.Printf("HTTPS服务器正在启动于端口 %s...", config.HTTPSPort)
		if err := httpSrv.RunTLS(":"+config.HTTPSPort, config.CertFile, config.KeyFile); err != nil {
			log.Printf("HTTPS服务器启动失败: %v", err)
		}
	}
}

func setupRoutes(r *gin.Engine) {
	r.GET("/getAllowedDomains", func(c *gin.Context) {
		c.JSON(200, gin.H{"allowedDomains": config.AllowedDomains})
	})

	r.GET("/getMail/:randomString", handleGetMail)
}

func handleGetMail(c *gin.Context) {
	mailHead := c.Param("randomString")

	mu.RLock() // 使用读锁提高并发性能
	mails, exists := mailBox[mailHead]
	if !exists || len(mails) == 0 {
		mu.RUnlock()
		c.JSON(201, gin.H{"mail": "没有邮件"})
		return
	}

	lastIndex := len(mails) - 1
	tmpMail := mails[lastIndex]
	mu.RUnlock()

	mu.Lock() // 仅在需要修改时使用写锁
	mailBox[mailHead] = mails[:lastIndex]
	mu.Unlock()

	c.JSON(200, gin.H{
		"mail": gin.H{
			"from":        tmpMail.from,
			"title":       tmpMail.title,
			"TextContent": tmpMail.TextContent,
			"HtmlContent": tmpMail.HtmlContent,
		},
	})
}

func scheduleDailyMidnightTask(task func()) {
	ticker := time.NewTicker(24 * time.Hour)
	go func() {
		for {
			now := time.Now()
			nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
			duration := nextMidnight.Sub(now)

			timer := time.NewTimer(duration)
			select {
			case <-timer.C:
				task()
			case <-ticker.C:
				// 防止错过执行
				task()
			}
		}
	}()
}

func clearMailBox() {
	mu.Lock()
	defer mu.Unlock()
	mailBox = make(map[string][]mailContent)
	log.Printf("邮箱已在 %s 清空", time.Now().Format("2006-01-02 15:04:05"))
}

func main() {
	// 初始化配置
	config = initConfig()

	// 设置日志格式
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// 启动定时清理任务
	scheduleDailyMidnightTask(clearMailBox)

	// 启动 HTTP 服务器
	go startHTTPServer()

	// 启动 SMTP 服务器
	if err := startSMTPServer(); err != nil {
		log.Fatalf("SMTP服务器启动失败: %v", err)
	}
}
