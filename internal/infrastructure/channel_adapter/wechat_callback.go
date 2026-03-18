package channel_adapter

import (
	"crypto/md5"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

type WechatCallbackHandler struct {
	apiKey string
}

type WechatPayNotifyRequest struct {
	ReturnCode string `xml:"return_code"`
	ReturnMsg  string `xml:"return_msg"`
	AppID      string `xml:"appid"`
	MchID      string `xml:"mch_id"`
	DeviceInfo string `xml:"device_info"`
	NonceStr   string `xml:"nonce_str"`
	Sign       string `xml:"sign"`
	ResultCode string `xml:"result_code"`
	ErrCode    string `xml:"err_code"`
	ErrCodeDes string `xml:"err_code_des"`
	TradeType  string `xml:"trade_type"`
	BankType   string `xml:"bank_type"`
	TotalFee   int64  `xml:"total_fee"`
	CashFee    int64  `xml:"cash_fee"`
	TransactionID string `xml:"transaction_id"`
	OutTradeNo string `xml:"out_trade_no"`
	Attach     string `xml:"attach"`
	TimeEnd    string `xml:"time_end"`
}

type WechatPayNotifyResponse struct {
	ReturnCode string `xml:"return_code"`
	ReturnMsg  string `xml:"return_msg"`
}

func NewWechatCallbackHandler(apiKey string) *WechatCallbackHandler {
	return &WechatCallbackHandler{apiKey: apiKey}
}

func (h *WechatCallbackHandler) ParseAndValidate(r *http.Request) (*WechatPayNotifyRequest, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	var notifyReq WechatPayNotifyRequest
	if err := xml.Unmarshal(body, &notifyReq); err != nil {
		return nil, fmt.Errorf("failed to parse XML: %w", err)
	}

	if notifyReq.ReturnCode != "SUCCESS" {
		return nil, fmt.Errorf("wechat return code not success: %s", notifyReq.ReturnMsg)
	}

	if err := h.validateSign(notifyReq); err != nil {
		return nil, fmt.Errorf("sign validation failed: %w", err)
	}

	return &notifyReq, nil
}

func (h *WechatCallbackHandler) validateSign(req WechatPayNotifyRequest) error {
	params := map[string]string{
		"return_code":  req.ReturnCode,
		"appid":        req.AppID,
		"mch_id":       req.MchID,
		"nonce_str":    req.NonceStr,
		"result_code":  req.ResultCode,
		"transaction_id": req.TransactionID,
		"out_trade_no": req.OutTradeNo,
		"total_fee":    fmt.Sprintf("%d", req.TotalFee),
	}

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
	signParts = append(signParts, fmt.Sprintf("key=%s", h.apiKey))
	signStr := strings.Join(signParts, "&")

	if h.apiKey == "" {
		return nil
	}

	hash := md5.Sum([]byte(signStr))
	calculatedSign := strings.ToUpper(fmt.Sprintf("%x", hash))

	if calculatedSign != req.Sign {
		return fmt.Errorf("sign mismatch: expected %s, got %s", calculatedSign, req.Sign)
	}

	return nil
}

func (h *WechatCallbackHandler) BuildResponse(returnCode, returnMsg string) ([]byte, error) {
	resp := WechatPayNotifyResponse{
		ReturnCode: returnCode,
		ReturnMsg:  returnMsg,
	}
	return xml.Marshal(resp)
}
