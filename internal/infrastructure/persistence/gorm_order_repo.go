package persistence

import (
	"context"
	"errors"

	"aegis-pay/internal/domain/transaction"

	"gorm.io/gorm"
)

type GORMOrderRepository struct {
	db *gorm.DB
}

func NewGORMOrderRepository(db *gorm.DB) *GORMOrderRepository {
	return &GORMOrderRepository{db: db}
}

func (r *GORMOrderRepository) Save(ctx context.Context, order *transaction.PaymentOrder) error {
	po := toPaymentOrderPO(order)
	return r.db.WithContext(ctx).Create(po).Error
}

func (r *GORMOrderRepository) Update(ctx context.Context, order *transaction.PaymentOrder) error {
	return r.db.WithContext(ctx).Model(&PaymentOrderPO{}).
		Where("trade_no = ?", order.TradeNo).
		Updates(map[string]interface{}{
			"status":           order.Status,
			"channel_trade_no": order.ChannelTradeNo,
			"pay_url":          order.PayURL,
			"expired_at":       order.ExpiredAt,
			"success_at":       order.SuccessAt,
			"updated_at":       order.UpdatedAt,
		}).Error
}

func (r *GORMOrderRepository) FindByTradeNo(ctx context.Context, tradeNo string) (*transaction.PaymentOrder, error) {
	var po PaymentOrderPO
	err := r.db.WithContext(ctx).Where("trade_no = ?", tradeNo).First(&po).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toPaymentOrderEntity(&po), nil
}

func (r *GORMOrderRepository) FindByOutTradeNo(ctx context.Context, merchantID, outTradeNo string) (*transaction.PaymentOrder, error) {
	var po PaymentOrderPO
	err := r.db.WithContext(ctx).Where("merchant_id = ? AND out_trade_no = ?", merchantID, outTradeNo).First(&po).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toPaymentOrderEntity(&po), nil
}

func (r *GORMOrderRepository) UpdateStatus(ctx context.Context, tradeNo string, oldStatus, newStatus transaction.PaymentStatus) error {
	result := r.db.WithContext(ctx).Model(&PaymentOrderPO{}).
		Where("trade_no = ? AND status = ?", tradeNo, oldStatus).
		Update("status", newStatus)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("乐观锁更新失败：订单状态已被其他操作修改")
	}
	return nil
}

func toPaymentOrderPO(order *transaction.PaymentOrder) *PaymentOrderPO {
	return &PaymentOrderPO{
		ID:             0,
		TradeNo:        order.TradeNo,
		OutTradeNo:     order.OutTradeNo,
		MerchantID:     order.MerchantID,
		AppID:          order.AppID,
		Amount:         order.Amount,
		Status:         string(order.Status),
		Description:    order.Description,
		ChannelCode:    order.ChannelCode,
		ChannelTradeNo: order.ChannelTradeNo,
		PayURL:         order.PayURL,
		ExpiredAt:      order.ExpiredAt,
		SuccessAt:      order.SuccessAt,
		CreatedAt:      order.CreatedAt,
		UpdatedAt:      order.UpdatedAt,
	}
}

func toPaymentOrderEntity(po *PaymentOrderPO) *transaction.PaymentOrder {
	return &transaction.PaymentOrder{
		TradeNo:        po.TradeNo,
		OutTradeNo:     po.OutTradeNo,
		MerchantID:     po.MerchantID,
		AppID:          po.AppID,
		Amount:         po.Amount,
		Status:         transaction.PaymentStatus(po.Status),
		Description:    po.Description,
		ChannelCode:    po.ChannelCode,
		ChannelTradeNo: po.ChannelTradeNo,
		PayURL:         po.PayURL,
		ExpiredAt:      po.ExpiredAt,
		SuccessAt:      po.SuccessAt,
		CreatedAt:      po.CreatedAt,
		UpdatedAt:      po.UpdatedAt,
	}
}

type GORMRefundRepository struct {
	db *gorm.DB
}

func NewGORMRefundRepository(db *gorm.DB) *GORMRefundRepository {
	return &GORMRefundRepository{db: db}
}

func (r *GORMRefundRepository) Save(ctx context.Context, refund *transaction.RefundOrder) error {
	po := toRefundOrderPO(refund)
	return r.db.WithContext(ctx).Create(po).Error
}

func (r *GORMRefundRepository) Update(ctx context.Context, refund *transaction.RefundOrder) error {
	return r.db.WithContext(ctx).Model(&RefundOrderPO{}).
		Where("refund_no = ?", refund.RefundNo).
		Updates(map[string]interface{}{
			"status":            refund.Status,
			"channel_refund_no": refund.ChannelRefundNo,
			"success_at":        refund.SuccessAt,
			"updated_at":        refund.UpdatedAt,
		}).Error
}

func (r *GORMRefundRepository) FindByRefundNo(ctx context.Context, refundNo string) (*transaction.RefundOrder, error) {
	var po RefundOrderPO
	err := r.db.WithContext(ctx).Where("refund_no = ?", refundNo).First(&po).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toRefundOrderEntity(&po), nil
}

func (r *GORMRefundRepository) FindByOutRefundNo(ctx context.Context, merchantID, outRefundNo string) (*transaction.RefundOrder, error) {
	var po RefundOrderPO
	err := r.db.WithContext(ctx).Where("merchant_id = ? AND out_refund_no = ?", merchantID, outRefundNo).First(&po).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toRefundOrderEntity(&po), nil
}

func toRefundOrderPO(refund *transaction.RefundOrder) *RefundOrderPO {
	return &RefundOrderPO{
		ID:              0,
		RefundNo:        refund.RefundNo,
		TradeNo:         refund.TradeNo,
		OutRefundNo:     refund.OutRefundNo,
		MerchantID:      refund.MerchantID,
		AppID:           refund.AppID,
		RefundAmount:    refund.RefundAmount,
		Status:          string(refund.Status),
		Reason:          refund.Reason,
		ChannelRefundNo: refund.ChannelRefundNo,
		SuccessAt:       refund.SuccessAt,
		CreatedAt:       refund.CreatedAt,
		UpdatedAt:       refund.UpdatedAt,
	}
}

func toRefundOrderEntity(po *RefundOrderPO) *transaction.RefundOrder {
	return &transaction.RefundOrder{
		RefundNo:        po.RefundNo,
		TradeNo:         po.TradeNo,
		OutRefundNo:     po.OutRefundNo,
		MerchantID:      po.MerchantID,
		AppID:           po.AppID,
		RefundAmount:    po.RefundAmount,
		Status:          transaction.RefundStatus(po.Status),
		Reason:          po.Reason,
		ChannelRefundNo: po.ChannelRefundNo,
		SuccessAt:       po.SuccessAt,
		CreatedAt:       po.CreatedAt,
		UpdatedAt:       po.UpdatedAt,
	}
}

func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&PaymentOrderPO{}, &RefundOrderPO{}, &RiskReportPO{})
}

type DBManager struct {
	db *gorm.DB
}

func NewDBManager(db *gorm.DB) *DBManager {
	return &DBManager{db: db}
}

func (m *DBManager) GetDB() *gorm.DB {
	return m.db
}

func (m *DBManager) Close() error {
	sqlDB, err := m.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

type TxFunc func(ctx context.Context) error

func (m *DBManager) WithTransaction(ctx context.Context, fn TxFunc) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(ctx)
	})
}

func (m *DBManager) WithTransactionV2(fn func(tx *gorm.DB) error) error {
	return m.db.Transaction(fn)
}
