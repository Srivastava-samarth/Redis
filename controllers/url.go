package controller

import (
	"errors"
	"net/http"

	service "redis-lab/services"

	"github.com/gin-gonic/gin"
)

type URLController struct {
	service *service.URLService
}

func NewURLController(urlService *service.URLService) *URLController {
	return &URLController{
		service: urlService,
	}
}

type createURLRequest struct {
	URL string `json:"url" binding:"required"`
}

type createURLResponse struct {
	ShortCode string `json:"short_code"`
	URL       string `json:"url"`
}

type getURLResponse struct {
	URL string `json:"url"`
}

func (c *URLController) CreateURL(ctx *gin.Context) {
	var request createURLRequest

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	clientID := ctx.GetHeader("X-Client-ID")
	if clientID == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "X-Client-ID header is required",
		})
		return
	}

	idempotencyKey := ctx.GetHeader("Idempotency-Key")
	if idempotencyKey == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "Idempotency-Key header is required",
		})
		return
	}

	url, err := c.service.CreateURL(
		clientID,
		request.URL,
		idempotencyKey,
	)

	if err != nil {
		switch {
		case errors.Is(err, service.ErrRequestProcessing):
			ctx.JSON(http.StatusConflict, gin.H{
				"error": "request is already being processed",
			})

		case errors.Is(err, service.ErrRateLimitExceeded):
			ctx.JSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})

		default:
			ctx.JSON(http.StatusInternalServerError, gin.H{
				"error": "failed to create URL",
			})
		}

		return
	}

	ctx.JSON(http.StatusOK, createURLResponse{
		ShortCode: url.ShortCode,
		URL:       url.OriginalURL,
	})
}

func (c *URLController) GetURL(ctx *gin.Context) {
	shortCode := ctx.Param("shortCode")

	if shortCode == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "short code is required",
		})
		return
	}

	url, err := c.service.GetURL(shortCode)

	if err != nil {
		ctx.JSON(http.StatusNotFound, gin.H{
			"error": "URL not found",
		})
		return
	}

	ctx.JSON(http.StatusOK, getURLResponse{
		URL: url.OriginalURL,
	})
}
