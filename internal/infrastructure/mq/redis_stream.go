package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis Streams 常量定义
const (
	NotifyStreamKey = "notify_stream"      // 商户通知队列的 Stream key
	NotifyGroup     = "aegis_notify_group" // 消费者组名称
	NotifyConsumer  = "aegis_consumer"     // 消费者名称
	RiskStreamKey   = "risk_event_stream"
	RiskGroup       = "aegis_risk_group"
	RiskConsumer    = "aegis_risk_consumer"
)

// NotifyMessage 商户 Webhook 通知消息
// 支付成功后，通过 Redis Streams 异步投递通知任务给商户
type NotifyMessage struct {
	ID          string `json:"id"`            // 消息ID
	MerchantID  string `json:"merchant_id"`   // 商户号
	AppID       string `json:"app_id"`        // 应用ID
	OutTradeNo  string `json:"out_trade_no"`  // 商户订单号
	TradeNo     string `json:"trade_no"`      // 系统流水号
	Status      string `json:"status"`        // 订单状态
	Amount      int64  `json:"amount"`        // 金额(分)
	NotifyURL   string `json:"notify_url"`    // 商户通知地址
	RetryCount  int    `json:"retry_count"`   // 重试次数
	NextRetryAt int64  `json:"next_retry_at"` // 下次重试时间戳
	CreatedAt   int64  `json:"created_at"`    // 创建时间戳
}

type RiskEventMessage struct {
	ID             string                 `json:"id"`
	TradeNo        string                 `json:"trade_no"`
	MerchantID     string                 `json:"merchant_id"`
	Amount         int64                  `json:"amount"`
	ChannelCode    string                 `json:"channel_code"`
	OccurredAt     int64                  `json:"occurred_at"`
	TransactionCtx map[string]interface{} `json:"transaction_ctx"`
}

// RedisStreamMQ 基于 Redis Streams 的消息队列实现
// 使用 XADD 发布消息，XREADGROUP 消费，XACK 确认
type RedisStreamMQ struct {
	client *redis.Client
}

// NewRedisStreamMQ 构造函数
func NewRedisStreamMQ(client *redis.Client) *RedisStreamMQ {
	return &RedisStreamMQ{client: client}
}

// InitStream 初始化 Stream 和 Consumer Group
// 如果 Group 已存在则忽略错误
func (m *RedisStreamMQ) InitStream(ctx context.Context) error {
	err := m.client.XGroupCreateMkStream(ctx, NotifyStreamKey, NotifyGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

func (m *RedisStreamMQ) InitRiskStream(ctx context.Context) error {
	err := m.client.XGroupCreateMkStream(ctx, RiskStreamKey, RiskGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

// PublishNotify 发布通知消息到 Stream
// 使用 XADD 指令，确保消息持久化
func (m *RedisStreamMQ) PublishNotify(ctx context.Context, msg *NotifyMessage) error {
	msg.CreatedAt = time.Now().Unix()
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = m.client.XAdd(ctx, &redis.XAddArgs{
		Stream: NotifyStreamKey,
		Values: map[string]interface{}{
			"data": string(data),
		},
	}).Result()
	return err
}

// Consume 消费消息
// 使用 XREADGROUP 从 Consumer Group 读取新消息
// 返回消息列表和消息ID列表（用于后续ACK）
func (m *RedisStreamMQ) Consume(ctx context.Context, count int64) ([]*NotifyMessage, []string, error) {
	results, err := m.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    NotifyGroup,
		Consumer: NotifyConsumer,
		Streams:  []string{NotifyStreamKey, ">"}, // ">" 表示只读新消息
		Count:    count,
	}).Result()
	if err != nil {
		return nil, nil, err
	}

	var messages []*NotifyMessage
	var ids []string

	// 解析消息
	for _, stream := range results {
		for _, item := range stream.Messages {
			dataStr, ok := item.Values["data"].(string)
			if !ok {
				continue
			}
			var payload NotifyMessage
			if err := json.Unmarshal([]byte(dataStr), &payload); err != nil {
				continue
			}
			payload.ID = item.ID
			messages = append(messages, &payload)
			ids = append(ids, item.ID)
		}
	}

	return messages, ids, nil
}

// Ack 确认消息已处理
// 使用 XACK 将消息从 PEL (Pending Entries List) 移除
func (m *RedisStreamMQ) Ack(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return m.client.XAck(ctx, NotifyStreamKey, NotifyGroup, ids...).Err()
}

// GetPendingMessages 获取待处理消息（PEL中的消息）
// 用于商户服务器宕机后恢复未确认的消息，实现延迟重试
func (m *RedisStreamMQ) GetPendingMessages(ctx context.Context) ([]*NotifyMessage, []string, error) {
	results, err := m.client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: NotifyStreamKey,
		Group:  NotifyGroup,
		Start:  "-",
		End:    "+",
		Count:  100,
	}).Result()
	if err != nil {
		return nil, nil, err
	}

	var messages []*NotifyMessage
	var ids []string

	for _, pending := range results {
		msg, err := m.getMessageByID(ctx, pending.ID)
		if err != nil {
			continue
		}
		if msg != nil {
			messages = append(messages, msg)
			ids = append(ids, pending.ID)
		}
	}

	return messages, ids, nil
}

// getMessageByID 根据消息ID从 Stream 中获取消息内容
func (m *RedisStreamMQ) getMessageByID(ctx context.Context, id string) (*NotifyMessage, error) {
	results, err := m.client.XRange(ctx, NotifyStreamKey, id, id).Result()
	if err != nil || len(results) == 0 {
		return nil, nil
	}
	dataStr, ok := results[0].Values["data"].(string)
	if !ok {
		return nil, nil
	}
	var msg NotifyMessage
	if err := json.Unmarshal([]byte(dataStr), &msg); err != nil {
		return nil, err
	}
	msg.ID = id
	return &msg, nil
}

// RequeueMessage 重新入队消息（用于重试）
// 增加重试次数，设置下次重试时间，重新发布消息
func (m *RedisStreamMQ) RequeueMessage(ctx context.Context, id string, delaySeconds int64) error {
	msg, err := m.getMessageByID(ctx, id)
	if err != nil || msg == nil {
		return fmt.Errorf("message not found: %s", id)
	}
	msg.RetryCount++
	msg.NextRetryAt = time.Now().Add(time.Duration(delaySeconds) * time.Second).Unix()
	return m.PublishNotify(ctx, msg)
}

func (m *RedisStreamMQ) PublishRiskEvent(ctx context.Context, event *RiskEventMessage) error {
	event.OccurredAt = time.Now().Unix()
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = m.client.XAdd(ctx, &redis.XAddArgs{
		Stream: RiskStreamKey,
		MaxLen: 100000,
		Approx: true,
		Values: map[string]interface{}{
			"data": string(data),
		},
	}).Result()
	return err
}

func (m *RedisStreamMQ) ConsumeRiskEvents(ctx context.Context, count int64) ([]*RiskEventMessage, []string, error) {
	results, err := m.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    RiskGroup,
		Consumer: RiskConsumer,
		Streams:  []string{RiskStreamKey, ">"},
		Count:    count,
	}).Result()
	if err != nil {
		return nil, nil, err
	}

	var events []*RiskEventMessage
	var ids []string
	for _, stream := range results {
		for _, item := range stream.Messages {
			dataStr, ok := item.Values["data"].(string)
			if !ok {
				continue
			}
			var event RiskEventMessage
			if err := json.Unmarshal([]byte(dataStr), &event); err != nil {
				continue
			}
			event.ID = item.ID
			events = append(events, &event)
			ids = append(ids, item.ID)
		}
	}

	return events, ids, nil
}

func (m *RedisStreamMQ) AckRiskEvents(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return m.client.XAck(ctx, RiskStreamKey, RiskGroup, ids...).Err()
}
