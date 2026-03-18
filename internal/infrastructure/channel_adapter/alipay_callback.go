package channel_adapter

import (
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type AlipayCallbackHandler struct {
	appID       string
	alipayPubKey string
}

type AlipayTradeNotification struct {
	NotifyTime    string `json:"notify_time"`
	NotifyType    string `json:"notify_type"`
	NotifyID      string `json:"notify_id"`
	AppID        string `json:"app_id"`
	Charset      string `json:"charset"`
	Version      string `json:"version"`
	SignType     string `json:"sign_type"`
	Sign         string `json:"sign"`
	TradeNo      string `json:"trade_no"`
	OutTradeNo   string `json:"out_trade_no"`
	OutBizNo     string `json:"out_biz_no"`
	BizType      string `json:"biz_type"`
	TradeStatus  string `json:"trade_status"`
	TotalAmount  string `json:"total_amount"`
	ReceiptAmount string `json:"receipt_amount"`
	BuyerPayAmount string `json:"buyer_pay_amount"`
	PointAmount  string `json:"point_amount"`
	InvoiceAmount string `json:"invoice_amount"`
	GmtCreate    string `json:"gmt_create"`
	GmtPayment   string `json:"gmt_payment"`
	SellerID     string `json:"seller_id"`
	SellerEmail  string `json:"seller_email"`
	BuyerID      string `json:"buyer_id"`
	BuyerLogID   string `json:"buyer_log_id"`
	BuyerName    string `json:"buyer_name"`
}

type AlipayTradeResponse struct {
	Code    string `json:"code"`
	Msg     string `json:"msg"`
	OutTradeNo string `json:"out_trade_no"`
	TradeNo string `json:"trade_no"`
}

func NewAlipayCallbackHandler(appID, alipayPubKey string) *AlipayCallbackHandler {
	return &AlipayCallbackHandler{
		appID:       appID,
		alipayPubKey: alipayPubKey,
	}
}

func (h *AlipayCallbackHandler) ParseAndValidate(r *http.Request) (*AlipayTradeNotification, error) {
	if err := r.ParseForm(); err != nil {
		return nil, fmt.Errorf("failed to parse form: %w", err)
	}

	notifyParams := make(map[string]string)
	for key, values := range r.PostForm {
		if len(values) > 0 {
			notifyParams[key] = values[0]
		}
	}

	if err := h.validateSign(notifyParams); err != nil {
		return nil, fmt.Errorf("sign validation failed: %w", err)
	}

	notification := &AlipayTradeNotification{}
	notification.NotifyTime = notifyParams["notify_time"]
	notification.NotifyType = notifyParams["notify_type"]
	notification.NotifyID = notifyParams["notify_id"]
	notification.AppID = notifyParams["app_id"]
	notification.Charset = notifyParams["charset"]
	notification.Version = notifyParams["version"]
	notification.SignType = notifyParams["sign_type"]
	notification.Sign = notifyParams["sign"]
	notification.TradeNo = notifyParams["trade_no"]
	notification.OutTradeNo = notifyParams["out_trade_no"]
	notification.BizType = notifyParams["biz_type"]
	notification.TradeStatus = notifyParams["trade_status"]
	notification.TotalAmount = notifyParams["total_amount"]
	notification.ReceiptAmount = notifyParams["receipt_amount"]
	notification.BuyerPayAmount = notifyParams["buyer_pay_amount"]
	notification.PointAmount = notifyParams["point_amount"]
	notification.InvoiceAmount = notifyParams["invoice_amount"]
	notification.GmtCreate = notifyParams["gmt_create"]
	notification.GmtPayment = notifyParams["gmt_payment"]
	notification.SellerID = notifyParams["seller_id"]
	notification.SellerEmail = notifyParams["seller_email"]
	notification.BuyerID = notifyParams["buyer_id"]

	return notification, nil
}

func (h *AlipayCallbackHandler) validateSign(params map[string]string) error {
	signType := params["sign_type"]
	sign := params["sign"]

	delete(params, "sign")
	delete(params, "sign_type")

	var dataToSign []string
	for key, value := range params {
		if value != "" {
			dataToSign = append(dataToSign, fmt.Sprintf("%s=%s", key, value))
		}
	}
	sort.Strings(dataToSign)
	signedData := strings.Join(dataToSign, "&")

	var signBytes []byte
	switch signType {
	case "RSA", "RSA2":
		pubKey, err := h.parsePublicKey(h.alipayPubKey)
		if err != nil {
			return fmt.Errorf("failed to parse alipay public key: %w", err)
		}

		signDecoded, err := base64.StdEncoding.DecodeString(sign)
		if err != nil {
			return fmt.Errorf("failed to decode sign: %w", err)
		}

		hashFunc := crypto.SHA256
		if signType == "RSA" {
			hashFunc = crypto.SHA1
		}

		var hash []byte
		hashed := hashFunc.New()
		hashed.Write([]byte(signedData))
		hash = hashed.Sum(nil)

		if signType == "RSA2" {
			err = rsa.VerifyPKCS1v15(pubKey.(*rsa.PublicKey), crypto.SHA256, hash, signDecoded)
		} else {
			err = rsa.VerifyPKCS1v15(pubKey.(*rsa.PublicKey), crypto.SHA1, hash, signDecoded)
		}

		if err != nil {
			return fmt.Errorf("RSA verify failed: %w", err)
		}
	default:
		return fmt.Errorf("unsupported sign_type: %s", signType)
	}

	_ = signedData
	_ = signBytes
	return nil
}

func (h *AlipayCallbackHandler) parsePublicKey(pubKeyStr string) (interface{}, error) {
	pubKeyStr = strings.ReplaceAll(pubKeyStr, "-----BEGIN PUBLIC KEY-----", "")
	pubKeyStr = strings.ReplaceAll(pubKeyStr, "-----END PUBLIC KEY-----", "")
	pubKeyStr = strings.ReplaceAll(pubKeyStr, " ", "")

	keyBytes, err := base64.StdEncoding.DecodeString(pubKeyStr)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	pub, err := x509.ParsePKIXPublicKey(keyBytes)
	if err != nil {
		pubInterface, err := x509.ParsePKCS1PublicKey(keyBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse public key: %w", err)
		}
		return pubInterface, nil
	}

	return pub, nil
}

func sortStrings(values []string) {
	for i := 0; i < len(values)-1; i++ {
		for j := i + 1; j < len(values); j++ {
			if values[i] > values[j] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}
