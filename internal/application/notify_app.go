package application

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"aegis-pay/internal/infrastructure/mq"
)

type NotifyApp struct {
	mq         *mq.RedisStreamMQ
	httpClient *http.Client
}

func NewNotifyApp(mq *mq.RedisStreamMQ) *NotifyApp {
	return &NotifyApp{
		mq: mq,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type MerchantNotifyPayload struct {
	TradeNo    string `json:"trade_no"`
	OutTradeNo string `json:"out_trade_no"`
	MerchantID string `json:"merchant_id"`
	AppID      string `json:"app_id"`
	Amount     int64  `json:"amount"`
	Status     string `json:"status"`
	SuccessAt  int64  `json:"success_at"`
	Sign       string `json:"sign"`
}

func (app *NotifyApp) StartConsumer(ctx context.Context) {
	go app.consumeLoop(ctx)
}

func (app *NotifyApp) consumeLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			app.processMessages(ctx)
		}
	}
}

func (app *NotifyApp) processMessages(ctx context.Context) {
	messages, ids, err := app.mq.Consume(ctx, 10)
	if err != nil {
		log.Printf("Failed to consume messages: %v", err)
		return
	}

	if len(messages) == 0 {
		return
	}

	for _, msg := range messages {
		if err := app.sendNotify(ctx, msg); err != nil {
			log.Printf("Failed to send notify for %s: %v", msg.TradeNo, err)
			continue
		}
	}

	if err := app.mq.Ack(ctx, ids); err != nil {
		log.Printf("Failed to ack messages: %v", err)
	}
}

func (app *NotifyApp) sendNotify(ctx context.Context, msg *mq.NotifyMessage) error {
	if msg.NotifyURL == "" {
		return fmt.Errorf("notify_url is empty")
	}

	payload := MerchantNotifyPayload{
		TradeNo:    msg.TradeNo,
		OutTradeNo: msg.OutTradeNo,
		MerchantID: msg.MerchantID,
		AppID:      msg.AppID,
		Amount:     msg.Amount,
		Status:     msg.Status,
		SuccessAt:  time.Now().Unix(),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", msg.NotifyURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (app *NotifyApp) ProcessPendingMessages(ctx context.Context) error {
	messages, ids, err := app.mq.GetPendingMessages(ctx)
	if err != nil {
		return fmt.Errorf("failed to get pending messages: %w", err)
	}

	for _, msg := range messages {
		if time.Now().Unix() < msg.NextRetryAt {
			continue
		}
		if err := app.sendNotify(ctx, msg); err != nil {
			log.Printf("Failed to send pending notify for %s: %v", msg.TradeNo, err)
			continue
		}
		if err := app.mq.Ack(ctx, []string{msg.ID}); err != nil {
			log.Printf("Failed to ack pending message: %v", err)
		}
	}

	_ = ids
	return nil
}
