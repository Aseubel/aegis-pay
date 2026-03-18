package application

import (
	"context"
	"fmt"
	"time"

	"aegis-pay/internal/domain/channel"
	"aegis-pay/internal/domain/transaction"
	"aegis-pay/internal/infrastructure/mq"
	"aegis-pay/internal/infrastructure/persistence"

	"gorm.io/gorm"
)

// PayApp 支付应用服务
// 负责用例编排，包含统一下单、支付回调处理、订单查询等核心业务逻辑
type PayApp struct {
	dbManager     *persistence.DBManager       // 数据库管理器
	orderRepo     transaction.OrderRepository  // 支付订单仓储
	refundRepo    transaction.RefundRepository // 退款订单仓储
	domainService *transaction.DomainService   // 领域服务
	gateway       channel.PaymentGateway       // 支付渠道网关
	mq            *mq.RedisStreamMQ            // Redis消息队列
	lockManager   *mq.LockManager              // 分布式锁管理器
}

// NewPayApp 构造函数，注入依赖
func NewPayApp(
	dbManager *persistence.DBManager,
	orderRepo transaction.OrderRepository,
	refundRepo transaction.RefundRepository,
	gateway channel.PaymentGateway,
	mq *mq.RedisStreamMQ,
	lockManager *mq.LockManager,
) *PayApp {
	return &PayApp{
		dbManager:   dbManager,
		orderRepo:   orderRepo,
		refundRepo:  refundRepo,
		gateway:     gateway,
		mq:          mq,
		lockManager: lockManager,
	}
}

// CreatePayOrderRequest 创建支付订单请求
type CreatePayOrderRequest struct {
	MerchantID    string `json:"merchant_id" binding:"required"`  // 商户号
	AppID         string `json:"app_id" binding:"required"`       // 应用ID
	OutTradeNo    string `json:"out_trade_no" binding:"required"` // 商户订单号
	Amount        int64  `json:"amount" binding:"required,gt=0"`  // 金额(分)
	Description   string `json:"description"`                     // 订单描述
	ChannelCode   string `json:"channel_code"`                    // 渠道编码，默认mock
	NotifyURL     string `json:"notify_url"`                      // 商户通知地址
	RedirectURL   string `json:"redirect_url"`                    // 支付后跳转地址
	ExpireMinutes int    `json:"expire_minutes"`                  // 订单过期分钟数
}

// CreatePayOrderResponse 创建支付订单响应
type CreatePayOrderResponse struct {
	TradeNo     string `json:"trade_no"`     // 系统流水号
	OutTradeNo  string `json:"out_trade_no"` // 商户订单号
	Amount      int64  `json:"amount"`       // 金额(分)
	PayURL      string `json:"pay_url"`      // 支付链接
	QRCodeURL   string `json:"qr_code_url"`  // 二维码链接
	ChannelCode string `json:"channel_code"` // 渠道编码
	ExpiredAt   int64  `json:"expired_at"`   // 过期时间戳
}

// CreatePayOrder 统一下单用例
// 流程：1. 创建订单( INIT ) -> 2. 调用渠道获取支付链接 -> 3. 更新订单为 PROCESSING
func (app *PayApp) CreatePayOrder(ctx context.Context, req *CreatePayOrderRequest) (*CreatePayOrderResponse, error) {
	// 生成系统流水号
	tradeNo := generateTradeNo()

	// 默认30分钟过期
	expireMinutes := req.ExpireMinutes
	if expireMinutes == 0 {
		expireMinutes = 30
	}

	// 构建支付订单实体，初始状态为 INIT
	order := &transaction.PaymentOrder{
		TradeNo:     tradeNo,
		OutTradeNo:  req.OutTradeNo,
		MerchantID:  req.MerchantID,
		AppID:       req.AppID,
		Amount:      req.Amount,
		Status:      transaction.StatusInit,
		Description: req.Description,
		ChannelCode: req.ChannelCode,
		ExpiredAt:   time.Now().Add(time.Duration(expireMinutes) * time.Minute),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// 开启事务保存订单
	if err := app.dbManager.WithTransactionV2(func(tx *gorm.DB) error {
		if err := app.orderRepo.Save(ctx, order); err != nil {
			return fmt.Errorf("failed to save order: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// 确定渠道编码
	channelCode := req.ChannelCode
	if channelCode == "" {
		channelCode = "mock"
	}

	// 确定通知地址
	notifyURL := req.NotifyURL
	if notifyURL == "" {
		notifyURL = "http://localhost:8080/webhooks/callback"
	}

	// 构建渠道支付请求
	payReq := &channel.PayRequest{
		TradeNo:       tradeNo,
		OutTradeNo:    req.OutTradeNo,
		Amount:        req.Amount,
		Description:   req.Description,
		NotifyURL:     notifyURL,
		RedirectURL:   req.RedirectURL,
		ExpireMinutes: expireMinutes,
	}

	// 调用渠道网关发起支付
	payResp, err := app.gateway.CreatePay(ctx, payReq)
	if err != nil {
		return nil, fmt.Errorf("failed to create pay: %w", err)
	}

	// 更新订单状态为 PROCESSING，记录支付链接和渠道交易号
	order.ToProcessing(payResp.PayURL, payResp.ChannelTradeNo)
	if err := app.orderRepo.Update(ctx, order); err != nil {
		return nil, fmt.Errorf("failed to update order: %w", err)
	}

	return &CreatePayOrderResponse{
		TradeNo:     tradeNo,
		OutTradeNo:  order.OutTradeNo,
		Amount:      order.Amount,
		PayURL:      payResp.PayURL,
		QRCodeURL:   payResp.QRCodeURL,
		ChannelCode: channelCode,
		ExpiredAt:   payResp.ExpiresAt,
	}, nil
}

// MockPaySuccess 模拟支付成功回调
// 用于测试环境：直接将订单标记为成功并发送商户通知
// 生产环境应使用真实的微信/支付宝回调接口
func (app *PayApp) MockPaySuccess(ctx context.Context, tradeNo string) error {
	// 获取分布式锁，防止同一订单并发处理
	lock := app.lockManager.NewLock(fmt.Sprintf("pay_callback:%s", tradeNo), 30*time.Second)
	acquired, err := lock.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	if !acquired {
		return fmt.Errorf("trade_no is being processed by another request")
	}
	defer lock.Release(ctx)

	// 查询订单
	order, err := app.orderRepo.FindByTradeNo(ctx, tradeNo)
	if err != nil {
		return fmt.Errorf("failed to find order: %w", err)
	}
	if order == nil {
		return fmt.Errorf("order not found: %s", tradeNo)
	}

	// 已成功的订单直接返回，避免重复处理
	if order.Status == transaction.StatusSuccess {
		return nil
	}

	// 检查订单是否过期，过期则关闭
	if order.IsExpired() {
		order.Close()
		app.orderRepo.Update(ctx, order)
		return fmt.Errorf("order has expired")
	}

	// 调用领域实体的 Success 方法，校验状态机流转
	if err := order.Success(); err != nil {
		return fmt.Errorf("failed to mark order as success: %w", err)
	}

	// 更新数据库
	if err := app.orderRepo.Update(ctx, order); err != nil {
		return fmt.Errorf("failed to update order: %w", err)
	}

	// 发布商户通知消息到 Redis Streams
	notifyMsg := &mq.NotifyMessage{
		MerchantID: order.MerchantID,
		AppID:      order.AppID,
		OutTradeNo: order.OutTradeNo,
		TradeNo:    order.TradeNo,
		Status:     string(order.Status),
		Amount:     order.Amount,
		NotifyURL:  fmt.Sprintf("http://localhost:8080/api/v1/notify/%s", order.MerchantID),
		RetryCount: 0,
		CreatedAt:  time.Now().Unix(),
	}
	if err := app.mq.PublishNotify(ctx, notifyMsg); err != nil {
		return fmt.Errorf("failed to publish notify message: %w", err)
	}

	return nil
}

// QueryOrderRequest 查询订单请求
type QueryOrderRequest struct {
	TradeNo    string // 系统流水号
	OutTradeNo string // 商户订单号
	MerchantID string // 商户号
}

// QueryOrderResponse 查询订单响应
type QueryOrderResponse struct {
	TradeNo        string `json:"trade_no"`             // 系统流水号
	OutTradeNo     string `json:"out_trade_no"`         // 商户订单号
	Amount         int64  `json:"amount"`               // 金额(分)
	Status         string `json:"status"`               // 订单状态
	ChannelCode    string `json:"channel_code"`         // 渠道编码
	ChannelTradeNo string `json:"channel_trade_no"`     // 渠道交易号
	CreatedAt      int64  `json:"created_at"`           // 创建时间
	SuccessAt      *int64 `json:"success_at,omitempty"` // 成功时间
}

// QueryOrder 查询订单用例
// 支持通过 trade_no 或 (merchant_id + out_trade_no) 组合查询
func (app *PayApp) QueryOrder(ctx context.Context, req *QueryOrderRequest) (*QueryOrderResponse, error) {
	var order *transaction.PaymentOrder
	var err error

	// 根据请求参数选择查询方式
	if req.TradeNo != "" {
		order, err = app.orderRepo.FindByTradeNo(ctx, req.TradeNo)
	} else if req.MerchantID != "" && req.OutTradeNo != "" {
		order, err = app.orderRepo.FindByOutTradeNo(ctx, req.MerchantID, req.OutTradeNo)
	} else {
		return nil, fmt.Errorf("either trade_no or (merchant_id + out_trade_no) is required")
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query order: %w", err)
	}
	if order == nil {
		return nil, fmt.Errorf("order not found")
	}

	// 构建响应
	resp := &QueryOrderResponse{
		TradeNo:        order.TradeNo,
		OutTradeNo:     order.OutTradeNo,
		Amount:         order.Amount,
		Status:         string(order.Status),
		ChannelCode:    order.ChannelCode,
		ChannelTradeNo: order.ChannelTradeNo,
		CreatedAt:      order.CreatedAt.Unix(),
	}
	if order.SuccessAt != nil {
		successAt := order.SuccessAt.Unix()
		resp.SuccessAt = &successAt
	}
	return resp, nil
}

// WechatCallbackRequest 微信支付回调请求参数
type WechatCallbackRequest struct {
	TransactionID string // 微信交易号
	OutTradeNo    string // 商户订单号
	TotalFee      int64  // 订单金额(分)
	TimeEnd       string // 支付完成时间
}

// ProcessWechatCallback 处理微信支付回调
func (app *PayApp) ProcessWechatCallback(ctx context.Context, req *WechatCallbackRequest) error {
	// 获取分布式锁，防止并发处理
	lock := app.lockManager.NewLock(fmt.Sprintf("pay_callback:wechat:%s", req.OutTradeNo), 30*time.Second)
	acquired, err := lock.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	if !acquired {
		return fmt.Errorf("trade_no is being processed by another request")
	}
	defer lock.Release(ctx)

	// 通过商户订单号查询订单
	order, err := app.orderRepo.FindByOutTradeNo(ctx, "", req.OutTradeNo)
	if err != nil {
		return fmt.Errorf("failed to find order: %w", err)
	}
	if order == nil {
		return fmt.Errorf("order not found: %s", req.OutTradeNo)
	}

	// 已成功的订单直接返回，避免重复处理
	if order.Status == transaction.StatusSuccess {
		return nil
	}

	// 检查订单是否过期
	if order.IsExpired() {
		order.Close()
		app.orderRepo.Update(ctx, order)
		return fmt.Errorf("order has expired")
	}

	// 金额校验：防止金额被篡改
	if order.Amount != req.TotalFee {
		return fmt.Errorf("amount mismatch: order=%d, callback=%d", order.Amount, req.TotalFee)
	}

	// 调用领域实体的 Success 方法，校验状态机流转
	if err := order.Success(); err != nil {
		return fmt.Errorf("failed to mark order as success: %w", err)
	}

	// 更新数据库
	if err := app.orderRepo.Update(ctx, order); err != nil {
		return fmt.Errorf("failed to update order: %w", err)
	}

	// 发布商户通知消息到 Redis Streams
	notifyMsg := &mq.NotifyMessage{
		MerchantID: order.MerchantID,
		AppID:      order.AppID,
		OutTradeNo: order.OutTradeNo,
		TradeNo:    order.TradeNo,
		Status:     string(order.Status),
		Amount:     order.Amount,
		NotifyURL:  fmt.Sprintf("http://localhost:8080/api/v1/notify/%s", order.MerchantID),
		RetryCount: 0,
		CreatedAt:  time.Now().Unix(),
	}
	if err := app.mq.PublishNotify(ctx, notifyMsg); err != nil {
		return fmt.Errorf("failed to publish notify message: %w", err)
	}

	return nil
}

// AlipayCallbackRequest 支付宝回调请求参数
type AlipayCallbackRequest struct {
	TradeNo     string // 支付宝交易号
	OutTradeNo  string // 商户订单号
	TotalAmount string // 订单金额(元)
	GmtPayment  string // 支付时间
}

// ProcessAlipayCallback 处理支付宝回调
func (app *PayApp) ProcessAlipayCallback(ctx context.Context, req *AlipayCallbackRequest) error {
	// 获取分布式锁，防止并发处理
	lock := app.lockManager.NewLock(fmt.Sprintf("pay_callback:alipay:%s", req.OutTradeNo), 30*time.Second)
	acquired, err := lock.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	if !acquired {
		return fmt.Errorf("trade_no is being processed by another request")
	}
	defer lock.Release(ctx)

	// 通过商户订单号查询订单
	order, err := app.orderRepo.FindByOutTradeNo(ctx, "", req.OutTradeNo)
	if err != nil {
		return fmt.Errorf("failed to find order: %w", err)
	}
	if order == nil {
		return fmt.Errorf("order not found: %s", req.OutTradeNo)
	}

	// 已成功的订单直接返回，避免重复处理
	if order.Status == transaction.StatusSuccess {
		return nil
	}

	// 检查订单是否过期
	if order.IsExpired() {
		order.Close()
		app.orderRepo.Update(ctx, order)
		return fmt.Errorf("order has expired")
	}

	// 金额校验：支付宝金额是元，需要转换为分
	var amountCent int64
	if req.TotalAmount != "" {
		var amountYuan float64
		fmt.Sscanf(req.TotalAmount, "%f", &amountYuan)
		amountCent = int64(amountYuan * 100)
	}
	if order.Amount != amountCent {
		return fmt.Errorf("amount mismatch: order=%d, callback=%d", order.Amount, amountCent)
	}

	// 调用领域实体的 Success 方法，校验状态机流转
	if err := order.Success(); err != nil {
		return fmt.Errorf("failed to mark order as success: %w", err)
	}

	// 更新数据库
	if err := app.orderRepo.Update(ctx, order); err != nil {
		return fmt.Errorf("failed to update order: %w", err)
	}

	// 发布商户通知消息到 Redis Streams
	notifyMsg := &mq.NotifyMessage{
		MerchantID: order.MerchantID,
		AppID:      order.AppID,
		OutTradeNo: order.OutTradeNo,
		TradeNo:    order.TradeNo,
		Status:     string(order.Status),
		Amount:     order.Amount,
		NotifyURL:  fmt.Sprintf("http://localhost:8080/api/v1/notify/%s", order.MerchantID),
		RetryCount: 0,
		CreatedAt:  time.Now().Unix(),
	}
	if err := app.mq.PublishNotify(ctx, notifyMsg); err != nil {
		return fmt.Errorf("failed to publish notify message: %w", err)
	}

	return nil
}

// generateTradeNo 生成系统流水号
// 格式: AP + 毫秒时间戳 + 纳秒后3位
func generateTradeNo() string {
	return fmt.Sprintf("AP%d%d", time.Now().UnixNano()/1000000, time.Now().Nanosecond()%1000)
}
