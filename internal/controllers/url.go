package handler

import (
	"net/http"
	service "redis-lab/internal/services"

	"github.com/gin-gonic/gin"
)

type CreateURLRequest struct {
	ShortCode   *string `json:"short_code" binding:"required"`
	OriginalURL *string `json:"original_url" binding:"required,url"`
}

type URLHandler struct {
	service *service.URLService
}

func NewURLHandler(service *service.URLService) *URLHandler {
	return &URLHandler{
		service: service,
	}
}

func (h *URLHandler) CreateURL() gin.HandlerFunc {

	return func(c *gin.Context) {

		var req CreateURLRequest

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": err.Error(),
			})
			return
		}

		url, err := h.service.CreateURL(
			c.Request.Context(),
			*req.ShortCode,
			*req.OriginalURL,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": err.Error(),
			})
			return
		}

		c.JSON(http.StatusCreated, url)
	}
}

func (h *URLHandler) GetURL() gin.HandlerFunc {

	return func(c *gin.Context) {

		shortCode := c.Param("shortCode")

		url, source, err := h.service.GetURL(
			c.Request.Context(),
			shortCode,
		)

		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "URL not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"id":           url.ID,
			"short_code":   url.ShortCode,
			"original_url": url.OriginalURL,
			"source":       source,
		})
	}
}