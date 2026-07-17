package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *handlers) CreateTelegramBindToken(c *gin.Context) {
	scope, ok := h.settingsScope(c)
	if !ok {
		return
	}
	if h.telegramBinding == nil {
		_ = c.Error(errors.New("telegram integration unavailable"))
		return
	}
	link, err := h.telegramBinding.IssueBindToken(c.Request.Context(), scope)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": link.Token, "deep_link": link.DeepLink})
}
