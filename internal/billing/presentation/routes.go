package presentation

import (
	"github.com/gin-gonic/gin"

	authapp "github.com/okok/harbor-services/internal/auth/application"
	billingapp "github.com/okok/harbor-services/internal/billing/application"
)

// RegisterRoutes mounts Billing REST and webhook routes.
func RegisterRoutes(r *gin.Engine, svc *billingapp.Service, auth *authapp.Service) {
	h := NewHandlers(svc, auth)

	secretOnly := RequireAppSecret(svc)
	secretOrJWT := RequireAppSecretOrJWT(svc, auth)

	billing := r.Group("/api/v1/billing")
	{
		billing.POST("/checkouts", secretOnly, h.CreateCheckout)

		billing.GET("/orders/:order_id", secretOrJWT, h.GetOrder)
		billing.GET("/orders", secretOrJWT, h.ListOrders)

		billing.POST("/products", secretOnly, h.CreateProduct)
		billing.GET("/products", secretOnly, h.ListProducts)
		billing.GET("/products/:product_id", secretOnly, h.GetProduct)
		billing.PUT("/products/:product_id", secretOnly, h.UpdateProduct)
	}

	// Per-app webhook URL for tenant binding: /webhooks/billing/{provider}/{app_id}
	r.POST("/webhooks/billing/:provider/:app_id", h.HandleWebhook)
}
