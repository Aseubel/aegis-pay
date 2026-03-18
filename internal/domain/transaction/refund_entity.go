package transaction

import (
	"errors"
	"time"
)

type RefundStatus string

const (
	RefundStatusInit       RefundStatus = "INIT"
	RefundStatusProcessing RefundStatus = "PROCESSING"
	RefundStatusSuccess    RefundStatus = "SUCCESS"
	RefundStatusFailed     RefundStatus = "FAILED"
)

type RefundOrder struct {
	RefundNo        string       // 系统退款流水号
	TradeNo         string       // 原支付流水号
	OutRefundNo     string       // 商户退款单号
	MerchantID      string       // 商户号
	AppID           string       // 应用ID
	RefundAmount    int64        // 退款金额(分)
	Status          RefundStatus // 退款状态
	Reason          string       // 退款原因
	ChannelRefundNo string       // 渠道退款单号
	SuccessAt       *time.Time   // 退款成功时间
	CreatedAt       time.Time    // 创建时间
	UpdatedAt       time.Time    // 更新时间
}

func (r *RefundOrder) Success() error {
	if r.Status != RefundStatusProcessing {
		return errors.New("退款状态不是处理中，无法标记为成功")
	}
	now := time.Now()
	r.Status = RefundStatusSuccess
	r.SuccessAt = &now
	r.UpdatedAt = now
	return nil
}

func (r *RefundOrder) Fail() error {
	if r.Status != RefundStatusProcessing {
		return errors.New("退款状态不是处理中，无法标记为失败")
	}
	r.Status = RefundStatusFailed
	r.UpdatedAt = time.Now()
	return nil
}

func (r *RefundOrder) ToProcessing() error {
	if r.Status != RefundStatusInit {
		return errors.New("只有初始化状态的退款单才能变为处理中")
	}
	r.Status = RefundStatusProcessing
	r.UpdatedAt = time.Now()
	return nil
}
