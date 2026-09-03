package presentation

import (
	"strings"

	"github.com/gin-gonic/gin"

	billingapp "github.com/okok/harbor-services/internal/billing/application"
	"github.com/okok/harbor-services/internal/billing/domain"
	"github.com/okok/harbor-services/internal/platform/apperr"
	"github.com/okok/harbor-services/internal/platform/response"
)

func (h *Handlers) CreateCheckout(c *gin.Context) {
	var req struct {
		ProductID         string         `json:"product_id"`
		ProviderProductID string         `json:"provider_product_id"`
		SuccessURL        string         `json:"success_url"`
		CancelURL         string         `json:"cancel_url"`
		UserID            string         `json:"user_id"`
		CustomerEmail     string         `json:"customer_email"`
		Provider          string         `json:"provider"`
		Metadata          map[string]any `json:"metadata"`
		IdempotencyKey    string         `json:"idempotency_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	res, err := h.svc.CreateCheckout(c.Request.Context(), appIDFromCtx(c), billingapp.CreateCheckoutInput{
		ProductID:         req.ProductID,
		ProviderProductID: req.ProviderProductID,
		SuccessURL:        req.SuccessURL,
		CancelURL:         req.CancelURL,
		UserID:            req.UserID,
		CustomerEmail:     req.CustomerEmail,
		Provider:          req.Provider,
		Metadata:          req.Metadata,
		IdempotencyKey:    req.IdempotencyKey,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *Handlers) GetOrder(c *gin.Context) {
	orderID := c.Param("order_id")
	o, err := h.svc.GetOrder(c.Request.Context(), appIDFromCtx(c), orderID, jwtUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, o)
}

func (h *Handlers) ListOrders(c *gin.Context) {
	userFilter := c.Query("user_id")
	if authMode(c) == AuthModeJWT {
		userFilter = "" // ignored; service forces jwt sub
	}
	status := domain.OrderStatus(c.Query("status"))
	orders, err := h.svc.ListOrders(c.Request.Context(), appIDFromCtx(c), jwtUserID(c), userFilter, status, 50)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"orders": orders})
}

func (h *Handlers) CreateProduct(c *gin.Context) {
	var req struct {
		Name             string            `json:"name"`
		Description      string            `json:"description"`
		Type             domain.ProductType `json:"type"`
		ProviderPriceIDs map[string]string `json:"provider_price_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	p, err := h.svc.CreateProduct(c.Request.Context(), appIDFromCtx(c), billingapp.CreateProductInput{
		Name:             req.Name,
		Description:      req.Description,
		Type:             req.Type,
		ProviderPriceIDs: req.ProviderPriceIDs,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, p)
}

func (h *Handlers) ListProducts(c *gin.Context) {
	includeArchived := c.Query("include_archived") == "true"
	list, err := h.svc.ListProducts(c.Request.Context(), appIDFromCtx(c), includeArchived)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"products": list})
}

func (h *Handlers) GetProduct(c *gin.Context) {
	p, err := h.svc.GetProduct(c.Request.Context(), appIDFromCtx(c), c.Param("product_id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, p)
}

func (h *Handlers) UpdateProduct(c *gin.Context) {
	var req struct {
		Name             *string              `json:"name"`
		Description      *string              `json:"description"`
		Type             *domain.ProductType  `json:"type"`
		ProviderPriceIDs map[string]string    `json:"provider_price_ids"`
		Status           *domain.ProductStatus `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	p, err := h.svc.UpdateProduct(c.Request.Context(), appIDFromCtx(c), c.Param("product_id"), billingapp.UpdateProductInput{
		Name:             req.Name,
		Description:      req.Description,
		Type:             req.Type,
		ProviderPriceIDs: req.ProviderPriceIDs,
		Status:           req.Status,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, p)
}

func (h *Handlers) HandleWebhook(c *gin.Context) {
	provider := strings.TrimSpace(c.Param("provider"))
	appID := strings.TrimSpace(c.Param("app_id"))
	if provider == "" || appID == "" {
		response.Fail(c, apperr.Validation("provider and app_id required"))
		return
	}
	body, err := readBody(c)
	if err != nil {
		response.Fail(c, apperr.Validation("failed to read body"))
		return
	}
	headers := map[string]string{}
	for k, vals := range c.Request.Header {
		if len(vals) > 0 {
			headers[k] = vals[0]
		}
	}
	if err := h.svc.HandleWebhook(c.Request.Context(), provider, appID, headers, body); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"received": true})
}
