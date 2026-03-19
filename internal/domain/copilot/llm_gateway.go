package copilot

import "context"

type DataAssistantGateway interface {
	AskData(ctx context.Context, merchantID string, question string) (answer string, err error)
}

type RiskAnalyzerGateway interface {
	Analyze(ctx context.Context, transactionCtx map[string]interface{}) (*RiskReport, error)
}
