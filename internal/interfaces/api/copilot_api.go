package api

import (
	"net/http"

	"aegis-pay/internal/application"

	"github.com/gin-gonic/gin"
)

type CopilotHandler struct {
	copilotApp *application.CopilotApp
}

func NewCopilotHandler(copilotApp *application.CopilotApp) *CopilotHandler {
	return &CopilotHandler{copilotApp: copilotApp}
}

type askDataRequest struct {
	MerchantID string `json:"merchant_id" binding:"required"`
	Question   string `json:"question" binding:"required"`
}

func (h *CopilotHandler) AskData(c *gin.Context) {
	var req askDataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "参数错误: " + err.Error(),
		})
		return
	}

	resp, err := h.copilotApp.AskData(c.Request.Context(), req.MerchantID, req.Question)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "查询失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "success",
		"data":    resp,
	})
}

func (h *CopilotHandler) RegisterRoutes(router *gin.RouterGroup) {
	router.POST("/copilot/query", h.AskData)
}
