package transaction

import (
	"errors"
	"time"
)

// PaymentStatus 支付订单状态枚举
// 状态流转: INIT -> PROCESSING -> SUCCESS/FAILED/CLOSED
type PaymentStatus string

const (
	StatusInit       PaymentStatus = "INIT"       // 初始化：已创建订单，等待第三方渠道
	StatusProcessing PaymentStatus = "PROCESSING" // 处理中：已向渠道发起支付
	StatusSuccess    PaymentStatus = "SUCCESS"    // 成功（终态）
	StatusFailed     PaymentStatus = "FAILED"     // 失败（终态）
	StatusClosed     PaymentStatus = "CLOSED"     // 关闭（终态）
)

// PaymentOrder 支付订单聚合根
// 维护支付订单的全生命周期状态机
type PaymentOrder struct {
	TradeNo        string        // 系统流水号 (全局唯一)
	OutTradeNo     string        // 商户订单号
	MerchantID     string        // 商户号
	AppID          string        // 应用ID
	Amount         int64         // 金额(分)，避免浮点精度问题
	Status         PaymentStatus // 订单状态
	Description    string        // 订单描述
	ChannelCode    string        // 渠道编码 (wechat/alipay/stripe/mock)
	ChannelTradeNo string        // 渠道交易号
	PayURL         string        // 支付链接/二维码Token
	ExpiredAt      time.Time     // 订单过期时间
	SuccessAt      *time.Time    // 成功时间
	CreatedAt      time.Time     // 创建时间
	UpdatedAt      time.Time     // 更新时间
}

// Success 将订单标记为成功
// 业务规则：只有 PROCESSING 状态的订单才能变为 SUCCESS
func (p *PaymentOrder) Success() error {
	if p.Status != StatusProcessing {
		return errors.New("订单状态不是处理中，无法标记为成功")
	}
	now := time.Now()
	p.Status = StatusSuccess
	p.SuccessAt = &now
	p.UpdatedAt = now
	return nil
}

// Fail 将订单标记为失败
// 业务规则：INIT 或 PROCESSING 状态的订单才能变为 FAILED
func (p *PaymentOrder) Fail() error {
	if p.Status != StatusProcessing && p.Status != StatusInit {
		return errors.New("订单状态不允许变为失败")
	}
	p.Status = StatusFailed
	p.UpdatedAt = time.Now()
	return nil
}

// Close 关闭订单
// 业务规则：已成功的订单不允许关闭
func (p *PaymentOrder) Close() error {
	if p.Status == StatusSuccess {
		return errors.New("已成功的订单不允许关闭")
	}
	p.Status = StatusClosed
	p.UpdatedAt = time.Now()
	return nil
}

// ToProcessing 将订单状态变更为处理中
// 同时记录支付链接和渠道交易号
func (p *PaymentOrder) ToProcessing(payURL string, channelTradeNo string) error {
	if p.Status != StatusInit {
		return errors.New("只有初始化状态的订单才能变为处理中")
	}
	p.Status = StatusProcessing
	p.PayURL = payURL
	p.ChannelTradeNo = channelTradeNo
	p.UpdatedAt = time.Now()
	return nil
}

// IsExpired 检查订单是否已过期
func (p *PaymentOrder) IsExpired() bool {
	return time.Now().After(p.ExpiredAt)
}

// CanChangeTo 校验状态流转是否合法
// 确保状态机单向不可逆
func (p *PaymentOrder) CanChangeTo(targetStatus PaymentStatus) bool {
	switch p.Status {
	case StatusInit:
		return targetStatus == StatusProcessing || targetStatus == StatusClosed
	case StatusProcessing:
		return targetStatus == StatusSuccess || targetStatus == StatusFailed || targetStatus == StatusClosed
	default:
		return false
	}
}
