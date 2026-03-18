package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aegis-pay/internal/application"
	"aegis-pay/internal/config"
	"aegis-pay/internal/infrastructure/channel_adapter"
	"aegis-pay/internal/infrastructure/mq"
	"aegis-pay/internal/infrastructure/persistence"
	"aegis-pay/internal/interfaces/api"
	"aegis-pay/internal/interfaces/webhooks"

	"github.com/gin-gonic/gin"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// main 应用入口
func main() {
	if err := run(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// run 应用启动流程
// 配置加载优先级：.env > config.yaml > 默认值
func run() error {
	// 加载配置（优先级：.env > yml > 默认值）
	config.LoadEnvFile()
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// 初始化 MySQL 数据库
	db, err := initDB(cfg.Database)
	if err != nil {
		return err
	}

	// 自动迁移数据库表结构
	if err := persistence.AutoMigrate(db); err != nil {
		return err
	}

	// 初始化 Redis 客户端
	redisClient := initRedis(cfg.Redis)

	// 初始化分布式锁管理器和消息队列
	lockManager := mq.NewLockManager(redisClient)
	redisMQ := mq.NewRedisStreamMQ(redisClient)
	if err := redisMQ.InitStream(context.Background()); err != nil {
		log.Printf("Warning: Failed to init redis stream: %v", err)
	}

	// 初始化仓储
	orderRepo := persistence.NewGORMOrderRepository(db)
	refundRepo := persistence.NewGORMRefundRepository(db)
	dbManager := persistence.NewDBManager(db)

	// 初始化支付渠道适配器（使用 Mock 适配器用于测试）
	mockAdapter := channel_adapter.NewMockAdapter()

	// 初始化应用服务
	payApp := application.NewPayApp(dbManager, orderRepo, refundRepo, mockAdapter, redisMQ, lockManager)
	notifyApp := application.NewNotifyApp(redisMQ)

	// 初始化 HTTP 处理器
	orderHandler := api.NewOrderHandler(payApp)
	webhookHandler := webhooks.NewWebhookHandler(payApp)

	// 配置 Gin 路由
	gin.SetMode(gin.DebugMode)
	router := gin.Default()

	// 健康检查接口
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 注册 API 路由
	v1 := router.Group("/api/v1")
	orderHandler.RegisterRoutes(v1)

	// 注册 Webhook 路由
	webhookHandler.RegisterRoutes(router)

	// 启动商户通知消费者（后台运行）
	go notifyApp.StartConsumer(context.Background())

	// 启动 HTTP 服务
	addr := cfg.App.GetAddr()
	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		log.Printf("Starting server on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// 等待中断信号，实现优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// 给予 5 秒超时时间完成正在处理的请求
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return err
	}

	log.Println("Server exited properly")
	return nil
}

// initDB 初始化 MySQL 数据库连接
func initDB(cfg config.DatabaseConfig) (*gorm.DB, error) {
	dsn := cfg.GetDSN()
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// 配置连接池
	sqlDB.SetMaxIdleConns(10)           // 最大空闲连接数
	sqlDB.SetMaxOpenConns(100)          // 最大打开连接数
	sqlDB.SetConnMaxLifetime(time.Hour) // 连接最大生命周期

	return db, nil
}

// initRedis 初始化 Redis 客户端
func initRedis(cfg config.RedisConfig) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     cfg.GetAddr(),
		Password: cfg.Password,
		DB:       cfg.DB,
	})
}

// ProviderSet Google Wire 依赖注入集合（预留）
var ProviderSet = wire.NewSet(
	initDB,
	initRedis,
)
