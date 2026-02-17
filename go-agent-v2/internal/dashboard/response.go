package dashboard

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/multi-agent/go-agent-v2/pkg/logger"
)

// 统一响应辅助 (遵循 gin_handler 规范, DRY: 所有 handler 共用)。

func success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": data})
}

func badRequest(c *gin.Context, code, message string) {
	c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": gin.H{"code": code, "message": message}})
}

func notFound(c *gin.Context, message string) {
	c.JSON(http.StatusNotFound, gin.H{"success": false, "error": gin.H{"code": "not_found", "message": message}})
}

func serverError(c *gin.Context, err error) {
	logger.FromContext(c.Request.Context()).Error("internal error", logger.Any(logger.FieldError, err))
	c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": gin.H{"code": "internal_error", "message": "服务器内部错误"}})
}
