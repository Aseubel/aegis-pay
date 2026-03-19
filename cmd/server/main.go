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
	"aegis-pay/internal/domain/channel"
	"aegis-pay/internal/domain/transaction"
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

	// 使用 Wire 自动注入依赖
	app, err := InitializeApp(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize app: %w", err)
	}

	// 自动迁移数据库表结构
	if err := persistence.AutoMigrate(app.DBManager.GetDB()); err != nil {
		return err
	}

	// 初始化 Redis Stream
	if err := app.RedisMQ.InitStream(context.Background()); err != nil {
		log.Printf("Warning: Failed to init redis stream: %v", err)
	}

	// 配置 Gin 路由模式
	switch cfg.App.Mode {
	case "release":
		gin.SetMode(gin.ReleaseMode)
	case "test":
		gin.SetMode(gin.TestMode)
	default:
		gin.SetMode(gin.DebugMode)
	}
	router := gin.Default()

	// 健康检查接口
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// 注册 API 路由
	v1 := router.Group("/api/v1")
	app.OrderHandler.RegisterRoutes(v1)

	// 注册 Webhook 路由
	app.WebhookHandler.RegisterRoutes(router)

	// 启动商户通知消费者（后台运行）
	go app.NotifyApp.StartConsumer(context.Background())

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
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)

	return db, nil
}

// initRedis 初始化 Redis 客户端
func initRedis(cfg config.RedisConfig) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     cfg.GetAddr(),
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: cfg.PoolSize,
	})
}

// ProviderSet Google Wire 依赖注入集合
// Wire 会根据这个集合自动生成依赖注入代码
var ProviderSet = wire.NewSet(
	// 基础设施初始化函数
	initDB,
	initRedis,

	// 配置提取函数
	ProvideDatabaseConfig,
	ProvideRedisConfig,

	// 持久化层
	persistence.NewDBManager,
	persistence.NewGORMOrderRepository,
	persistence.NewGORMRefundRepository,

	// 接口到实现的映射
	wire.Bind(new(transaction.OrderRepository), new(*persistence.GORMOrderRepository)),
	wire.Bind(new(transaction.RefundRepository), new(*persistence.GORMRefundRepository)),
	wire.Bind(new(channel.PaymentGateway), new(*channel_adapter.MockAdapter)),

	// 消息队列和分布式锁
	mq.NewLockManager,
	mq.NewRedisStreamMQ,

	// 支付渠道适配器
	channel_adapter.NewMockAdapter,

	// 应用服务层
	application.NewPayApp,
	application.NewNotifyApp,

	// 接口层
	api.NewOrderHandler,
	webhooks.NewWebhookHandler,
)

// ProvideDatabaseConfig 从 config.Config 中提取 DatabaseConfig
func ProvideDatabaseConfig(cfg *config.Config) config.DatabaseConfig {
	return cfg.Database
}

// ProvideRedisConfig 从 config.Config 中提取 RedisConfig
func ProvideRedisConfig(cfg *config.Config) config.RedisConfig {
	return cfg.Redis
}
