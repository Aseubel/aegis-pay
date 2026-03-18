package transaction

import "context"

type DomainService struct {
	orderRepo  OrderRepository
	refundRepo RefundRepository
}

func NewDomainService(orderRepo OrderRepository, refundRepo RefundRepository) *DomainService {
	return &DomainService{
		orderRepo:  orderRepo,
		refundRepo: refundRepo,
	}
}

func (s *DomainService) CreatePaymentOrder(ctx context.Context, order *PaymentOrder) error {
	return s.orderRepo.Save(ctx, order)
}

func (s *DomainService) GetPaymentOrder(ctx context.Context, tradeNo string) (*PaymentOrder, error) {
	return s.orderRepo.FindByTradeNo(ctx, tradeNo)
}

func (s *DomainService) GetPaymentOrderByOutTradeNo(ctx context.Context, merchantID, outTradeNo string) (*PaymentOrder, error) {
	return s.orderRepo.FindByOutTradeNo(ctx, merchantID, outTradeNo)
}

func (s *DomainService) UpdatePaymentStatus(ctx context.Context, tradeNo string, oldStatus, newStatus PaymentStatus) error {
	return s.orderRepo.UpdateStatus(ctx, tradeNo, oldStatus, newStatus)
}

func (s *DomainService) MarkPaymentSuccess(ctx context.Context, order *PaymentOrder) error {
	if err := order.Success(); err != nil {
		return err
	}
	return s.orderRepo.Update(ctx, order)
}

func (s *DomainService) MarkPaymentFailed(ctx context.Context, order *PaymentOrder) error {
	if err := order.Fail(); err != nil {
		return err
	}
	return s.orderRepo.Update(ctx, order)
}

func (s *DomainService) ClosePaymentOrder(ctx context.Context, order *PaymentOrder) error {
	if err := order.Close(); err != nil {
		return err
	}
	return s.orderRepo.Update(ctx, order)
}

func (s *DomainService) CreateRefundOrder(ctx context.Context, refund *RefundOrder) error {
	return s.refundRepo.Save(ctx, refund)
}

func (s *DomainService) GetRefundOrder(ctx context.Context, refundNo string) (*RefundOrder, error) {
	return s.refundRepo.FindByRefundNo(ctx, refundNo)
}
