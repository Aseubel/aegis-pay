package channel_adapter

import (
	"context"
	"fmt"
	"time"

	"aegis-pay/internal/domain/channel"
)

type MockAdapter struct {
	enabled bool
}

func NewMockAdapter() *MockAdapter {
	return &MockAdapter{enabled: true}
}

func (a *MockAdapter) CreatePay(ctx context.Context, req *channel.PayRequest) (*channel.PayResponse, error) {
	channelTradeNo := fmt.Sprintf("MOCK_%d", time.Now().UnixNano())
	payURL := fmt.Sprintf("https://mock-pay.example.com/gateway?trade_no=%s&amount=%d", channelTradeNo, req.Amount)
	qrCodeURL := fmt.Sprintf("https://mock-pay.example.com/qrcode/%s", channelTradeNo)
	expireMinutes := req.ExpireMinutes
	if expireMinutes == 0 {
		expireMinutes = 30
	}

	return &channel.PayResponse{
		ChannelTradeNo: channelTradeNo,
		PayURL:         payURL,
		QRCodeURL:      qrCodeURL,
		PrepayID:       channelTradeNo,
		ExpiresAt:      time.Now().Add(time.Duration(expireMinutes) * time.Minute).Unix(),
	}, nil
}

func (a *MockAdapter) Query(ctx context.Context, tradeNo string) (*channel.QueryResponse, error) {
	return &channel.QueryResponse{
		ChannelTradeNo: fmt.Sprintf("MOCK_%s", tradeNo),
		Status:         "SUCCESS",
		Amount:         0,
		PaidAt:         time.Now().Unix(),
	}, nil
}

func (a *MockAdapter) Refund(ctx context.Context, req *channel.RefundRequest) (*channel.RefundResponse, error) {
	channelRefundNo := fmt.Sprintf("MOCK_REFUND_%d", time.Now().UnixNano())
	return &channel.RefundResponse{
		ChannelRefundNo: channelRefundNo,
		Status:          "SUCCESS",
		RefundAt:        time.Now().Unix(),
	}, nil
}
