package webhooks

import (
	"log"
	"net/http"

	"aegis-pay/internal/application"
	"aegis-pay/internal/config"
	"aegis-pay/internal/infrastructure/channel_adapter"

	"github.com/gin-gonic/gin"
)

type WebhookHandler struct {
	payApp                *application.PayApp
	wechatCallbackHandler *channel_adapter.WechatCallbackHandler
	alipayCallbackHandler *channel_adapter.AlipayCallbackHandler
}

func NewWebhookHandler(payApp *application.PayApp, cfg *config.Config) *WebhookHandler {
	return &WebhookHandler{
		payApp:                payApp,
		wechatCallbackHandler: channel_adapter.NewWechatCallbackHandler(cfg.Wechat.APIKey),
		alipayCallbackHandler: channel_adapter.NewAlipayCallbackHandler(cfg.Alipay.AppID, cfg.Alipay.AlipayPublicKey),
	}
}

type MockPaySuccessRequest struct {
	TradeNo string `json:"trade_no" binding:"required"`
}

// MockPaySuccess 模拟支付成功回调（测试用）
func (h *WebhookHandler) MockPaySuccess(c *gin.Context) {
	var req MockPaySuccessRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误: " + err.Error(),
		})
		return
	}

	err := h.payApp.MockPaySuccess(c.Request.Context(), req.TradeNo)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "处理失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
	})
}

// WechatCallback 微信支付回调
// 微信服务器会自动调用此接口通知支付结果
func (h *WebhookHandler) WechatCallback(c *gin.Context) {
	notifyReq, err := h.wechatCallbackHandler.ParseAndValidate(c.Request)
	if err != nil {
		log.Printf("Wechat callback validation failed: %v", err)
		c.String(http.StatusBadRequest, "FAIL")
		return
	}

	// 处理支付成功回调
	if notifyReq.ResultCode == "SUCCESS" {
		err := h.payApp.ProcessWechatCallback(c.Request.Context(), &application.WechatCallbackRequest{
			TransactionID: notifyReq.TransactionID,
			OutTradeNo:    notifyReq.OutTradeNo,
			TotalFee:      notifyReq.TotalFee,
			TimeEnd:       notifyReq.TimeEnd,
		})
		if err != nil {
			log.Printf("Process wechat callback failed: %v", err)
			c.String(http.StatusInternalServerError, "FAIL")
			return
		}
	}

	// 返回 SUCCESS 表示已收到回调
	response, _ := h.wechatCallbackHandler.BuildResponse("SUCCESS", "OK")
	c.Data(http.StatusOK, "text/xml; charset=utf-8", response)
}

// AlipayCallback 支付宝回调
// 支付宝服务器会自动调用此接口通知支付结果
func (h *WebhookHandler) AlipayCallback(c *gin.Context) {
	notification, err := h.alipayCallbackHandler.ParseAndValidate(c.Request)
	if err != nil {
		log.Printf("Alipay callback validation failed: %v", err)
		c.String(http.StatusBadRequest, "fail")
		return
	}

	// 处理支付成功回调
	if notification.TradeStatus == "TRADE_SUCCESS" || notification.TradeStatus == "TRADE_FINISHED" {
		err := h.payApp.ProcessAlipayCallback(c.Request.Context(), &application.AlipayCallbackRequest{
			TradeNo:     notification.TradeNo,
			OutTradeNo:  notification.OutTradeNo,
			TotalAmount: notification.TotalAmount,
			GmtPayment:  notification.GmtPayment,
		})
		if err != nil {
			log.Printf("Process alipay callback failed: %v", err)
			c.String(http.StatusInternalServerError, "fail")
			return
		}
	}

	c.String(http.StatusOK, "success")
}

func (h *WebhookHandler) RegisterRoutes(router *gin.Engine) {
	webhooks := router.Group("/webhooks")
	{
		webhooks.POST("/mock_pay_success", h.MockPaySuccess)
		webhooks.POST("/wechat", h.WechatCallback)
		webhooks.POST("/alipay", h.AlipayCallback)
	}
}
