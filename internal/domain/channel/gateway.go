package channel

import "context"

// PayRequest 发起支付的请求参数
// 由应用层构建，传递给 PaymentGateway
type PayRequest struct {
	TradeNo       string // 系统流水号
	OutTradeNo    string // 商户订单号
	Amount        int64  // 金额(分)
	Description   string // 订单描述
	NotifyURL     string // 回调通知地址
	RedirectURL   string // 支付完成后跳转地址
	ExpireMinutes int    // 过期时间(分钟)
}

// PayResponse 发起支付的响应结果
type PayResponse struct {
	ChannelTradeNo string // 渠道交易号
	PayURL         string // 支付链接/二维码
	QRCodeURL      string // 二维码链接
	PrepayID       string // 预支付ID
	ExpiresAt      int64  // 过期时间戳
}

// QueryResponse 查询支付的响应结果
type QueryResponse struct {
	ChannelTradeNo string // 渠道交易号
	Status         string // 支付状态
	Amount         int64  // 订单金额
	PaidAt         int64  // 支付时间
}

// RefundRequest 退款的请求参数
type RefundRequest struct {
	TradeNo         string // 系统流水号
	OutRefundNo     string // 商户退款单号
	RefundAmount    int64  // 退款金额(分)
	Reason          string // 退款原因
	OriginalTradeNo string // 原渠道交易号
}

// RefundResponse 退款的响应结果
type RefundResponse struct {
	ChannelRefundNo string // 渠道退款单号
	Status          string // 退款状态
	RefundAt        int64  // 退款时间
}

// PaymentGateway 第三方支付渠道的防腐层接口
// 屏蔽微信/支付宝/Stripe 等渠道的差异，提供统一抽象
type PaymentGateway interface {
	// CreatePay 向渠道发起支付，获取支付链接/二维码
	CreatePay(ctx context.Context, req *PayRequest) (*PayResponse, error)
	// Query 查询渠道支付状态
	Query(ctx context.Context, tradeNo string) (*QueryResponse, error)
	// Refund 向渠道发起退款
	Refund(ctx context.Context, req *RefundRequest) (*RefundResponse, error)
}
