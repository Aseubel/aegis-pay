package channel_adapter

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"aegis-pay/internal/domain/channel"
)

type WechatAdapter struct {
	appID       string
	mchID       string
	apiKey      string
	certPath    string
	callbackURL string
	client      *http.Client
}

type WechatPayRequest struct {
	AppID          string `xml:"appid"`
	MchID          string `xml:"mch_id"`
	NonceStr       string `xml:"nonce_str"`
	Sign           string `xml:"sign"`
	Body           string `xml:"body"`
	OutTradeNo     string `xml:"out_trade_no"`
	TotalFee       int64  `xml:"total_fee"`
	SpbillCreateIP string `xml:"spbill_create_ip"`
	NotifyURL      string `xml:"notify_url"`
	TradeType      string `xml:"trade_type"`
}

type WechatPayResponse struct {
	ReturnCode string `xml:"return_code"`
	ReturnMsg  string `xml:"return_msg"`
	AppID      string `xml:"appid,omitempty"`
	MchID      string `xml:"mch_id,omitempty"`
	DeviceInfo string `xml:"device_info,omitempty"`
	NonceStr   string `xml:"nonce_str,omitempty"`
	Sign       string `xml:"sign,omitempty"`
	ResultCode string `xml:"result_code,omitempty"`
	ErrCode    string `xml:"err_code,omitempty"`
	ErrCodeDes string `xml:"err_code_des,omitempty"`
	TradeType  string `xml:"trade_type,omitempty"`
	PrepayID   string `xml:"prepay_id,omitempty"`
	CodeURL    string `xml:"code_url,omitempty"`
}

func NewWechatAdapter(appID, mchID, apiKey, certPath, callbackURL string) *WechatAdapter {
	return &WechatAdapter{
		appID:       appID,
		mchID:       mchID,
		apiKey:      apiKey,
		certPath:    certPath,
		callbackURL: callbackURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (a *WechatAdapter) CreatePay(ctx context.Context, req *channel.PayRequest) (*channel.PayResponse, error) {
	wechatReq := WechatPayRequest{
		AppID:          a.appID,
		MchID:          a.mchID,
		NonceStr:       fmt.Sprintf("%d", time.Now().UnixNano()),
		Body:           req.Description,
		OutTradeNo:     req.OutTradeNo,
		TotalFee:       req.Amount,
		SpbillCreateIP: "127.0.0.1",
		NotifyURL:      req.NotifyURL,
		TradeType:      "NATIVE",
	}

	sign := a.signWechatRequest(&wechatReq)
	wechatReq.Sign = sign

	xmlData, err := xml.Marshal(wechatReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal wechat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.mch.weixin.qq.com/pay/unifiedorder", bytes.NewReader(xmlData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "text/xml")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var wechatResp WechatPayResponse
	if err := xml.Unmarshal(body, &wechatResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if wechatResp.ReturnCode != "SUCCESS" {
		return nil, fmt.Errorf("wechat pay failed: %s - %s", wechatResp.ReturnCode, wechatResp.ReturnMsg)
	}

	if wechatResp.ResultCode != "SUCCESS" {
		return nil, fmt.Errorf("wechat pay failed: %s - %s", wechatResp.ErrCode, wechatResp.ErrCodeDes)
	}

	expireMinutes := req.ExpireMinutes
	if expireMinutes == 0 {
		expireMinutes = 30
	}

	return &channel.PayResponse{
		ChannelTradeNo: wechatResp.PrepayID,
		PayURL:         wechatResp.CodeURL,
		QRCodeURL:      wechatResp.CodeURL,
		PrepayID:       wechatResp.PrepayID,
		ExpiresAt:      time.Now().Add(time.Duration(expireMinutes) * time.Minute).Unix(),
	}, nil
}

func (a *WechatAdapter) signWechatRequest(req *WechatPayRequest) string {
	params := make(map[string]string)
	params["appid"] = req.AppID
	params["mch_id"] = req.MchID
	params["nonce_str"] = req.NonceStr
	params["body"] = req.Body
	params["out_trade_no"] = req.OutTradeNo
	params["total_fee"] = fmt.Sprintf("%d", req.TotalFee)
	params["spbill_create_ip"] = req.SpbillCreateIP
	params["notify_url"] = req.NotifyURL
	params["trade_type"] = req.TradeType

	var keys []string
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var signParts []string
	for _, k := range keys {
		if params[k] != "" {
			signParts = append(signParts, fmt.Sprintf("%s=%s", k, params[k]))
		}
	}
	signParts = append(signParts, fmt.Sprintf("key=%s", a.apiKey))
	signStr := strings.Join(signParts, "&")

	hash := md5.Sum([]byte(signStr))
	return fmt.Sprintf("%x", hash)
}

func (a *WechatAdapter) Query(ctx context.Context, tradeNo string) (*channel.QueryResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (a *WechatAdapter) Refund(ctx context.Context, req *channel.RefundRequest) (*channel.RefundResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
