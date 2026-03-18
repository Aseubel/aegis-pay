package transaction

import "context"

type OrderRepository interface {
	Save(ctx context.Context, order *PaymentOrder) error
	Update(ctx context.Context, order *PaymentOrder) error
	FindByTradeNo(ctx context.Context, tradeNo string) (*PaymentOrder, error)
	FindByOutTradeNo(ctx context.Context, merchantID, outTradeNo string) (*PaymentOrder, error)
	UpdateStatus(ctx context.Context, tradeNo string, oldStatus, newStatus PaymentStatus) error
}

type RefundRepository interface {
	Save(ctx context.Context, refund *RefundOrder) error
	Update(ctx context.Context, refund *RefundOrder) error
	FindByRefundNo(ctx context.Context, refundNo string) (*RefundOrder, error)
	FindByOutRefundNo(ctx context.Context, merchantID, outRefundNo string) (*RefundOrder, error)
}
