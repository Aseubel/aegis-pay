package copilot

import "context"

type RiskReportRepository interface {
	Save(ctx context.Context, report *RiskReport) error
	FindLatestByTradeNo(ctx context.Context, tradeNo string) (*RiskReport, error)
}
