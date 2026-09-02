package presentation

import (
	"github.com/gin-gonic/gin"

	authapp "github.com/okok/harbor-services/internal/auth/application"
	"github.com/okok/harbor-services/internal/billing"
	"github.com/okok/harbor-services/internal/ops"
	tenantapp "github.com/okok/harbor-services/internal/tenant/application"
)

// RegisterRoutes mounts Admin routes under /api/v1/admin.
func RegisterRoutes(
	r *gin.Engine,
	tenant *tenantapp.AppService,
	auth *authapp.Service,
	billingSvc billing.ConfigService,
	opsSvc ops.ConfigService,
	adminAppID string,
	adminEmails []string,
) {
	h := NewHandlers(tenant, auth, billingSvc, opsSvc)
	admin := r.Group("/api/v1/admin", RequireAdmin(auth, adminAppID, adminEmails))
	{
		admin.POST("/apps", h.CreateApp)
		admin.GET("/apps", h.ListApps)
		admin.GET("/apps/:app_id", h.GetApp)
		admin.POST("/apps/:app_id", h.UpdateApp)
		admin.POST("/apps/:app_id/secret", h.RotateSecret)
		admin.DELETE("/apps/:app_id", h.DisableApp)

		admin.GET("/apps/:app_id/auth-config", h.GetAuthConfig)
		admin.PUT("/apps/:app_id/auth-config", h.PutAuthConfig)

		admin.GET("/apps/:app_id/billing-config", h.GetBillingConfig)
		admin.PUT("/apps/:app_id/billing-config", h.PutBillingConfig)

		admin.GET("/apps/:app_id/ops-config", h.GetOpsConfig)
		admin.PUT("/apps/:app_id/ops-config", h.PutOpsConfig)
	}
}
