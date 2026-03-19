package application

import (
	"context"
	"fmt"
	"log"
	"time"

	"aegis-pay/internal/domain/copilot"
	"aegis-pay/internal/infrastructure/mq"
)

type CopilotApp struct {
	service        *copilot.Service
	dataAssistant  copilot.DataAssistantGateway
	riskAnalyzer   copilot.RiskAnalyzerGateway
	riskReportRepo copilot.RiskReportRepository
	riskMQ         *mq.RedisStreamMQ
}

func NewCopilotApp(
	service *copilot.Service,
	dataAssistant copilot.DataAssistantGateway,
	riskAnalyzer copilot.RiskAnalyzerGateway,
	riskReportRepo copilot.RiskReportRepository,
	riskMQ *mq.RedisStreamMQ,
) *CopilotApp {
	return &CopilotApp{
		service:        service,
		dataAssistant:  dataAssistant,
		riskAnalyzer:   riskAnalyzer,
		riskReportRepo: riskReportRepo,
		riskMQ:         riskMQ,
	}
}

type AskDataResponse struct {
	Answer string `json:"answer"`
}

func (app *CopilotApp) AskData(ctx context.Context, merchantID, question string) (*AskDataResponse, error) {
	if merchantID == "" {
		return nil, fmt.Errorf("merchant_id is required")
	}
	if err := app.service.ValidateQuestion(question); err != nil {
		return nil, err
	}
	intent := app.service.NewQueryIntent(merchantID, question)
	start := time.Now()
	answer, err := app.dataAssistant.AskData(ctx, merchantID, question)
	if err != nil {
		return nil, err
	}
	intent.AnalysisResult = answer
	intent.ExecutionMillis = time.Since(start).Milliseconds()
	return &AskDataResponse{Answer: intent.AnalysisResult}, nil
}

type HighAmountTransactionEvent struct {
	TradeNo        string                 `json:"trade_no"`
	MerchantID     string                 `json:"merchant_id"`
	Amount         int64                  `json:"amount"`
	ChannelCode    string                 `json:"channel_code"`
	TransactionCtx map[string]interface{} `json:"transaction_ctx"`
}

func (app *CopilotApp) PublishHighAmountTransactionEvent(ctx context.Context, event *HighAmountTransactionEvent) error {
	if event == nil || event.TradeNo == "" {
		return fmt.Errorf("invalid risk event")
	}
	return app.riskMQ.PublishRiskEvent(ctx, &mq.RiskEventMessage{
		TradeNo:        event.TradeNo,
		MerchantID:     event.MerchantID,
		Amount:         event.Amount,
		ChannelCode:    event.ChannelCode,
		TransactionCtx: event.TransactionCtx,
	})
}

func (app *CopilotApp) StartRiskConsumer(ctx context.Context) {
	go app.consumeRiskLoop(ctx)
}

func (app *CopilotApp) consumeRiskLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			app.processRiskEvents(ctx)
		}
	}
}

func (app *CopilotApp) processRiskEvents(ctx context.Context) {
	events, ids, err := app.riskMQ.ConsumeRiskEvents(ctx, 20)
	if err != nil {
		return
	}
	if len(events) == 0 {
		return
	}

	ackIDs := make([]string, 0, len(ids))
	for i, event := range events {
		txCtx := map[string]interface{}{
			"trade_no":     event.TradeNo,
			"merchant_id":  event.MerchantID,
			"amount":       event.Amount,
			"channel_code": event.ChannelCode,
			"occurred_at":  event.OccurredAt,
		}
		for k, v := range event.TransactionCtx {
			txCtx[k] = v
		}
		report, err := app.riskAnalyzer.Analyze(ctx, txCtx)
		if err != nil {
			log.Printf("risk analyze failed for %s: %v", event.TradeNo, err)
			continue
		}
		report = app.service.NormalizeRiskReport(report)
		if report.TradeNo == "" {
			report.TradeNo = event.TradeNo
		}
		if err := app.riskReportRepo.Save(ctx, report); err != nil {
			log.Printf("risk report save failed for %s: %v", event.TradeNo, err)
			continue
		}
		if i < len(ids) {
			ackIDs = append(ackIDs, ids[i])
		}
	}

	if len(ackIDs) > 0 {
		_ = app.riskMQ.AckRiskEvents(ctx, ackIDs)
	}
}
