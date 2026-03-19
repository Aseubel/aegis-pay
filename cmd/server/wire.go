//go:build wireinject
// +build wireinject

package main

import (
	"aegis-pay/internal/application"
	"aegis-pay/internal/config"
	"aegis-pay/internal/infrastructure/channel_adapter"
	"aegis-pay/internal/infrastructure/mq"
	"aegis-pay/internal/infrastructure/persistence"
	"aegis-pay/internal/interfaces/api"
	"aegis-pay/internal/interfaces/webhooks"
	"github.com/google/wire"
)

// App 应用程序结构体，包含所有依赖
type App struct {
	Config         *config.Config
	DBManager      *persistence.DBManager
	OrderRepo      *persistence.GORMOrderRepository
	RefundRepo     *persistence.GORMRefundRepository
	RiskReportRepo *persistence.GORMRiskReportRepository
	LockManager    *mq.LockManager
	RedisMQ        *mq.RedisStreamMQ
	MockAdapter    *channel_adapter.MockAdapter
	PayApp         *application.PayApp
	NotifyApp      *application.NotifyApp
	CopilotApp     *application.CopilotApp
	OrderHandler   *api.OrderHandler
	CopilotHandler *api.CopilotHandler
	WebhookHandler *webhooks.WebhookHandler
}

// InitializeApp 使用 Wire 自动生成依赖注入代码
// cfg 参数由外部传入，不通过 Wire 创建
// wire.Build 会根据 ProviderSet 自动生成初始化代码
func InitializeApp(cfg *config.Config) (*App, error) {
	wire.Build(
		ProviderSet,
		NewApp,
	)
	return &App{}, nil
}

// NewApp 创建 App 实例
func NewApp(
	cfg *config.Config,
	dbManager *persistence.DBManager,
	orderRepo *persistence.GORMOrderRepository,
	refundRepo *persistence.GORMRefundRepository,
	riskReportRepo *persistence.GORMRiskReportRepository,
	lockManager *mq.LockManager,
	redisMQ *mq.RedisStreamMQ,
	mockAdapter *channel_adapter.MockAdapter,
	payApp *application.PayApp,
	notifyApp *application.NotifyApp,
	copilotApp *application.CopilotApp,
	orderHandler *api.OrderHandler,
	copilotHandler *api.CopilotHandler,
	webhookHandler *webhooks.WebhookHandler,
) *App {
	return &App{
		Config:         cfg,
		DBManager:      dbManager,
		OrderRepo:      orderRepo,
		RefundRepo:     refundRepo,
		RiskReportRepo: riskReportRepo,
		LockManager:    lockManager,
		RedisMQ:        redisMQ,
		MockAdapter:    mockAdapter,
		PayApp:         payApp,
		NotifyApp:      notifyApp,
		CopilotApp:     copilotApp,
		OrderHandler:   orderHandler,
		CopilotHandler: copilotHandler,
		WebhookHandler: webhookHandler,
	}
}
