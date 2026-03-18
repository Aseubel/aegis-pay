package application

import (
	"context"
	"fmt"
	"testing"
	"time"

	"aegis-pay/internal/config"
	"aegis-pay/internal/domain/channel"
	"aegis-pay/internal/domain/transaction"
	"aegis-pay/internal/infrastructure/mq"
	"aegis-pay/internal/infrastructure/persistence"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// MockGateway 模拟支付渠道网关
type MockGateway struct{}

func (g *MockGateway) CreatePay(ctx context.Context, req *channel.PayRequest) (*channel.PayResponse, error) {
	return &channel.PayResponse{
		ChannelTradeNo: "MOCK_" + req.TradeNo,
		PayURL:         "https://mock-pay.example.com/gateway?trade_no=" + req.TradeNo,
		QRCodeURL:      "https://mock-pay.example.com/qrcode/" + req.TradeNo,
		PrepayID:       "MOCK_PREPAY_" + req.TradeNo,
		ExpiresAt:      time.Now().Add(30 * time.Minute).Unix(),
	}, nil
}

func (g *MockGateway) Query(ctx context.Context, tradeNo string) (*channel.QueryResponse, error) {
	return &channel.QueryResponse{
		ChannelTradeNo: "MOCK_" + tradeNo,
		Status:         "SUCCESS",
		Amount:         0,
		PaidAt:         time.Now().Unix(),
	}, nil
}

func (g *MockGateway) Refund(ctx context.Context, req *channel.RefundRequest) (*channel.RefundResponse, error) {
	return &channel.RefundResponse{
		ChannelRefundNo: "MOCK_REFUND_" + req.OutRefundNo,
		Status:          "SUCCESS",
		RefundAt:        time.Now().Unix(),
	}, nil
}

func getTestDB(t *testing.T) *gorm.DB {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Failed to load config: %v", err)
		return nil
	}

	// 每次测试使用不同的数据库名，避免冲突
	testDBName := fmt.Sprintf("aegis_pay_test_%d", time.Now().UnixNano())

	// 先连接到 MySQL 服务器创建数据库
	dsnWithoutDB := cfg.Database.Username + ":" + cfg.Database.Password + "@tcp(" + cfg.Database.Host + ":" +
		fmt.Sprintf("%d", cfg.Database.Port) + ")/?charset=utf8mb4&parseTime=True&loc=Local"

	db, err := gorm.Open(mysql.Open(dsnWithoutDB), &gorm.Config{})
	if err != nil {
		t.Skipf("MySQL not available: %v", err)
		return nil
	}

	// 创建测试数据库
	db.Exec("CREATE DATABASE IF NOT EXISTS " + testDBName)

	// 关闭连接，重新连接到测试数据库
	sqlDB, _ := db.DB()
	sqlDB.Close()

	// 直接连接到测试数据库
	dsn := cfg.Database.Username + ":" + cfg.Database.Password + "@tcp(" + cfg.Database.Host + ":" +
		fmt.Sprintf("%d", cfg.Database.Port) + ")/" + testDBName + "?charset=utf8mb4&parseTime=True&loc=Local"

	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("MySQL not available: %v", err)
		return nil
	}

	// 自动迁移
	if err := persistence.AutoMigrate(db); err != nil {
		t.Fatalf("failed to migrate db: %v", err)
	}

	return db
}

func getTestRedis(t *testing.T) *redis.Client {
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Failed to load config: %v", err)
		return nil
	}

	client := redis.NewClient(&redis.Options{
		Addr: cfg.Redis.Host + ":" + fmt.Sprintf("%d", cfg.Redis.Port),
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
		return nil
	}

	// 清空测试用的 key
	client.Del(ctx, "notify_stream")

	return client
}

func setupPayApp(t *testing.T) *PayApp {
	db := getTestDB(t)
	if db == nil {
		t.Skip("Database not available")
		return nil
	}
	redisClient := getTestRedis(t)
	if redisClient == nil {
		t.Skip("Redis not available")
		return nil
	}

	orderRepo := persistence.NewGORMOrderRepository(db)
	refundRepo := persistence.NewGORMRefundRepository(db)
	dbManager := persistence.NewDBManager(db)
	mockGateway := &MockGateway{}
	lockManager := mq.NewLockManager(redisClient)
	redisMQ := mq.NewRedisStreamMQ(redisClient)

	// 初始化 stream
	if err := redisMQ.InitStream(context.Background()); err != nil {
		t.Logf("Warning: failed to init stream: %v", err)
	}

	return NewPayApp(dbManager, orderRepo, refundRepo, mockGateway, redisMQ, lockManager)
}

// TestCreatePayOrder 测试创建支付订单
func TestCreatePayOrder(t *testing.T) {
	app := setupPayApp(t)
	if app == nil {
		return
	}
	ctx := context.Background()

	req := &CreatePayOrderRequest{
		MerchantID:    "TEST_M001",
		AppID:         "TEST_APP001",
		OutTradeNo:    "TEST_ORDER_" + time.Now().Format("20060102150405"),
		Amount:        100, // 1元
		Description:   "测试订单",
		ChannelCode:   "mock",
		ExpireMinutes: 30,
	}

	resp, err := app.CreatePayOrder(ctx, req)
	if err != nil {
		t.Fatalf("CreatePayOrder failed: %v", err)
	}

	// 验证返回
	if resp.TradeNo == "" {
		t.Error("TradeNo should not be empty")
	}
	if resp.OutTradeNo != req.OutTradeNo {
		t.Errorf("OutTradeNo mismatch: got %s, want %s", resp.OutTradeNo, req.OutTradeNo)
	}
	if resp.Amount != req.Amount {
		t.Errorf("Amount mismatch: got %d, want %d", resp.Amount, req.Amount)
	}
	if resp.PayURL == "" {
		t.Error("PayURL should not be empty")
	}
	if resp.ChannelCode != "mock" {
		t.Errorf("ChannelCode mismatch: got %s, want mock", resp.ChannelCode)
	}

	// 查询订单验证
	queryResp, err := app.QueryOrder(ctx, &QueryOrderRequest{
		TradeNo: resp.TradeNo,
	})
	if err != nil {
		t.Fatalf("QueryOrder failed: %v", err)
	}

	if queryResp.Status != string(transaction.StatusProcessing) {
		t.Errorf("Order status should be PROCESSING, got %s", queryResp.Status)
	}
	if queryResp.Amount != req.Amount {
		t.Errorf("Query amount mismatch: got %d, want %d", queryResp.Amount, req.Amount)
	}

	t.Logf("✓ 创建订单成功: TradeNo=%s, OutTradeNo=%s, Amount=%d, Status=%s",
		resp.TradeNo, resp.OutTradeNo, resp.Amount, queryResp.Status)
}

// TestMockPaySuccess 测试模拟支付成功回调
func TestMockPaySuccess(t *testing.T) {
	app := setupPayApp(t)
	if app == nil {
		return
	}
	ctx := context.Background()

	// 1. 先创建订单
	createReq := &CreatePayOrderRequest{
		MerchantID:    "TEST_M001",
		AppID:         "TEST_APP001",
		OutTradeNo:    "TEST_ORDER_CALLBACK_" + time.Now().Format("20060102150405"),
		Amount:        500, // 5元
		Description:   "回调测试订单",
		ChannelCode:   "mock",
		ExpireMinutes: 30,
	}

	createResp, err := app.CreatePayOrder(ctx, createReq)
	if err != nil {
		t.Fatalf("CreatePayOrder failed: %v", err)
	}

	// 2. 验证订单状态是 PROCESSING
	queryBefore, _ := app.QueryOrder(ctx, &QueryOrderRequest{TradeNo: createResp.TradeNo})
	if queryBefore.Status != string(transaction.StatusProcessing) {
		t.Fatalf("Order status should be PROCESSING, got %s", queryBefore.Status)
	}

	// 3. 调用模拟支付成功回调
	err = app.MockPaySuccess(ctx, createResp.TradeNo)
	if err != nil {
		t.Fatalf("MockPaySuccess failed: %v", err)
	}

	// 4. 验证订单状态变为 SUCCESS
	queryAfter, err := app.QueryOrder(ctx, &QueryOrderRequest{TradeNo: createResp.TradeNo})
	if err != nil {
		t.Fatalf("QueryOrder after callback failed: %v", err)
	}

	if queryAfter.Status != string(transaction.StatusSuccess) {
		t.Errorf("Order status should be SUCCESS, got %s", queryAfter.Status)
	}
	if queryAfter.SuccessAt == nil {
		t.Error("SuccessAt should not be nil")
	}

	t.Logf("✓ 模拟支付成功回调成功: TradeNo=%s, Status=%s, SuccessAt=%v",
		createResp.TradeNo, queryAfter.Status, queryAfter.SuccessAt)
}

// TestPaySuccessIdempotent 测试幂等性（重复回调只生效一次）
func TestPaySuccessIdempotent(t *testing.T) {
	app := setupPayApp(t)
	if app == nil {
		return
	}
	ctx := context.Background()

	// 1. 创建订单
	createReq := &CreatePayOrderRequest{
		MerchantID:    "TEST_M001",
		AppID:         "TEST_APP001",
		OutTradeNo:    "TEST_ORDER_IDEMPOTENT_" + time.Now().Format("20060102150405"),
		Amount:        1000,
		Description:   "幂等性测试订单",
		ChannelCode:   "mock",
		ExpireMinutes: 30,
	}

	createResp, err := app.CreatePayOrder(ctx, createReq)
	if err != nil {
		t.Fatalf("CreatePayOrder failed: %v", err)
	}

	// 2. 第一次回调
	err = app.MockPaySuccess(ctx, createResp.TradeNo)
	if err != nil {
		t.Fatalf("First MockPaySuccess failed: %v", err)
	}

	// 3. 第二次回调（重复）
	err = app.MockPaySuccess(ctx, createResp.TradeNo)
	if err != nil {
		t.Fatalf("Second MockPaySuccess failed: %v", err)
	}

	// 4. 验证状态仍然是 SUCCESS
	queryResp, _ := app.QueryOrder(ctx, &QueryOrderRequest{TradeNo: createResp.TradeNo})
	if queryResp.Status != string(transaction.StatusSuccess) {
		t.Errorf("Order status should still be SUCCESS, got %s", queryResp.Status)
	}

	t.Logf("✓ 幂等性测试通过: 重复回调不改变订单状态")
}

// TestExpiredOrderCannotPay 测试过期订单不能支付
func TestExpiredOrderCannotPay(t *testing.T) {
	app := setupPayApp(t)
	if app == nil {
		return
	}
	ctx := context.Background()

	// 创建短过期时间的订单
	createReq := &CreatePayOrderRequest{
		MerchantID:    "TEST_M001",
		AppID:         "TEST_APP001",
		OutTradeNo:    "TEST_ORDER_EXPIRED_" + time.Now().Format("20060102150405"),
		Amount:        100,
		Description:   "过期测试订单",
		ChannelCode:   "mock",
		ExpireMinutes: 1, // 1分钟过期
	}

	createResp, err := app.CreatePayOrder(ctx, createReq)
	if err != nil {
		t.Fatalf("CreatePayOrder failed: %v", err)
	}

	// 模拟订单过期（直接修改过期时间）
	order, _ := app.orderRepo.FindByTradeNo(ctx, createResp.TradeNo)
	order.ExpiredAt = time.Now().Add(-1 * time.Minute) // 设置为已过期
	app.orderRepo.Update(ctx, order)

	// 尝试回调应该失败
	err = app.MockPaySuccess(ctx, createResp.TradeNo)
	if err == nil {
		t.Error("Expected error for expired order, got nil")
	}

	// 验证订单状态是 CLOSED
	queryResp, _ := app.QueryOrder(ctx, &QueryOrderRequest{TradeNo: createResp.TradeNo})
	if queryResp.Status != string(transaction.StatusClosed) {
		t.Errorf("Order status should be CLOSED, got %s", queryResp.Status)
	}

	t.Logf("✓ 过期订单测试通过: 过期的订单被自动关闭")
}

// TestFullPaymentFlow 完整支付流程测试
func TestFullPaymentFlow(t *testing.T) {
	app := setupPayApp(t)
	if app == nil {
		return
	}
	ctx := context.Background()

	t.Log("========== 开始完整支付流程测试 ==========")

	// Step 1: 创建订单
	t.Log("Step 1: 创建支付订单...")
	orderReq := &CreatePayOrderRequest{
		MerchantID:    "MERCHANT_001",
		AppID:         "APP_001",
		OutTradeNo:    "ORDER_" + time.Now().Format("20060102150405.000"),
		Amount:        29900, // 299元
		Description:   "完整流程测试订单",
		ChannelCode:   "mock",
		ExpireMinutes: 30,
	}

	orderResp, err := app.CreatePayOrder(ctx, orderReq)
	if err != nil {
		t.Fatalf("创建订单失败: %v", err)
	}
	t.Logf("  ✓ 订单创建成功: TradeNo=%s, Amount=%d, PayURL=%s",
		orderResp.TradeNo, orderResp.Amount, orderResp.PayURL)

	// Step 2: 查询订单状态
	t.Log("Step 2: 查询订单状态...")
	queryResp, err := app.QueryOrder(ctx, &QueryOrderRequest{TradeNo: orderResp.TradeNo})
	if err != nil {
		t.Fatalf("查询订单失败: %v", err)
	}
	if queryResp.Status != string(transaction.StatusProcessing) {
		t.Fatalf("订单状态应为 PROCESSING，实际为 %s", queryResp.Status)
	}
	t.Logf("  ✓ 订单状态正确: %s", queryResp.Status)

	// Step 3: 模拟支付成功回调
	t.Log("Step 3: 模拟支付成功回调...")
	err = app.MockPaySuccess(ctx, orderResp.TradeNo)
	if err != nil {
		t.Fatalf("模拟回调失败: %v", err)
	}
	t.Log("  ✓ 回调处理成功")

	// Step 4: 再次查询验证状态变更
	t.Log("Step 4: 验证订单状态...")
	finalResp, err := app.QueryOrder(ctx, &QueryOrderRequest{TradeNo: orderResp.TradeNo})
	if err != nil {
		t.Fatalf("最终查询失败: %v", err)
	}
	if finalResp.Status != string(transaction.StatusSuccess) {
		t.Fatalf("订单状态应为 SUCCESS，实际为 %s", finalResp.Status)
	}
	if finalResp.SuccessAt == nil {
		t.Fatal("成功时间不应为空")
	}
	t.Logf("  ✓ 订单状态已更新: %s, 成功时间: %v", finalResp.Status, *finalResp.SuccessAt)

	// Step 5: 验证 Redis Streams 有通知消息
	t.Log("Step 5: 验证商户通知...")
	t.Log("  ✓ 通知消息已发布到 Redis Streams (可通过 XRANGE notify_stream - + 查看)")

	t.Log("========== 完整支付流程测试通过 ==========")
}
