package persistence

import (
	"time"
)

// PaymentOrderPO 支付订单持久化对象
// 对应数据库表 payment_orders
type PaymentOrderPO struct {
	ID             uint       `gorm:"primaryKey"`                   // 主键
	TradeNo        string     `gorm:"uniqueIndex;size:64;not null"` // 系统流水号，唯一索引
	OutTradeNo     string     `gorm:"index;size:64;not null"`       // 商户订单号，普通索引
	MerchantID     string     `gorm:"index;size:32;not null"`       // 商户号，索引用于查询
	AppID          string     `gorm:"size:32;not null"`             // 应用ID
	Amount         int64      `gorm:"not null"`                     // 金额(分)，使用 int64 避免精度问题
	Status         string     `gorm:"size:20;not null"`             // 订单状态
	Description    string     `gorm:"size:255"`                     // 订单描述
	ChannelCode    string     `gorm:"size:20"`                      // 渠道编码
	ChannelTradeNo string     `gorm:"size:64"`                      // 渠道交易号
	PayURL         string     `gorm:"size:512"`                     // 支付链接
	ExpiredAt      time.Time  `gorm:"not null"`                     // 过期时间
	SuccessAt      *time.Time // 成功时间
	CreatedAt      time.Time  // 创建时间
	UpdatedAt      time.Time  // 更新时间
}

// TableName 指定表名为 payment_orders
func (PaymentOrderPO) TableName() string {
	return "payment_orders"
}

// RefundOrderPO 退款订单持久化对象
// 对应数据库表 refund_orders
type RefundOrderPO struct {
	ID              uint       `gorm:"primaryKey"`                   // 主键
	RefundNo        string     `gorm:"uniqueIndex;size:64;not null"` // 系统退款流水号，唯一索引
	TradeNo         string     `gorm:"index;size:64;not null"`       // 原支付流水号，索引
	OutRefundNo     string     `gorm:"uniqueIndex;size:64;not null"` // 商户退款单号，唯一索引
	MerchantID      string     `gorm:"index;size:32;not null"`       // 商户号
	AppID           string     `gorm:"size:32;not null"`             // 应用ID
	RefundAmount    int64      `gorm:"not null"`                     // 退款金额(分)
	Status          string     `gorm:"size:20;not null"`             // 退款状态
	Reason          string     `gorm:"size:255"`                     // 退款原因
	ChannelRefundNo string     `gorm:"size:64"`                      // 渠道退款单号
	SuccessAt       *time.Time // 退款成功时间
	CreatedAt       time.Time  // 创建时间
	UpdatedAt       time.Time  // 更新时间
}

// TableName 指定表名为 refund_orders
func (RefundOrderPO) TableName() string {
	return "refund_orders"
}
