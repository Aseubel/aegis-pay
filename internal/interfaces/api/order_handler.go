package api

import (
	"net/http"

	"aegis-pay/internal/application"

	"github.com/gin-gonic/gin"
)

type OrderHandler struct {
	payApp *application.PayApp
}

func NewOrderHandler(payApp *application.PayApp) *OrderHandler {
	return &OrderHandler{payApp: payApp}
}

func (h *OrderHandler) CreateOrder(c *gin.Context) {
	var req application.CreatePayOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误: " + err.Error(),
		})
		return
	}

	resp, err := h.payApp.CreatePayOrder(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建订单失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data":    resp,
	})
}

func (h *OrderHandler) QueryOrder(c *gin.Context) {
	tradeNo := c.Query("trade_no")
	outTradeNo := c.Query("out_trade_no")
	merchantID := c.Query("merchant_id")

	req := &application.QueryOrderRequest{
		TradeNo:    tradeNo,
		OutTradeNo: outTradeNo,
		MerchantID: merchantID,
	}

	resp, err := h.payApp.QueryOrder(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "查询订单失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data":    resp,
	})
}

func (h *OrderHandler) RegisterRoutes(router *gin.RouterGroup) {
	router.POST("/orders", h.CreateOrder)
	router.GET("/orders", h.QueryOrder)
}
