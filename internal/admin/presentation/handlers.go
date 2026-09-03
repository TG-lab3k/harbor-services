package presentation

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/gin-gonic/gin"

	authapp "github.com/okok/harbor-services/internal/auth/application"
	"github.com/okok/harbor-services/internal/auth/domain"
	authpres "github.com/okok/harbor-services/internal/auth/presentation"
	"github.com/okok/harbor-services/internal/billing"
	"github.com/okok/harbor-services/internal/ops"
	"github.com/okok/harbor-services/internal/platform/apperr"
	"github.com/okok/harbor-services/internal/platform/response"
	tenantapp "github.com/okok/harbor-services/internal/tenant/application"
	tenantdomain "github.com/okok/harbor-services/internal/tenant/domain"
)

// Handlers exposes Admin HTTP adapters.
type Handlers struct {
	tenant  *tenantapp.AppService
	auth    *authapp.Service
	billing billing.ConfigService
	ops     ops.ConfigService
}

func NewHandlers(
	tenant *tenantapp.AppService,
	auth *authapp.Service,
	billingSvc billing.ConfigService,
	opsSvc ops.ConfigService,
) *Handlers {
	return &Handlers{tenant: tenant, auth: auth, billing: billingSvc, ops: opsSvc}
}

func (h *Handlers) CreateApp(c *gin.Context) {
	var req struct {
		AppName      string         `json:"app_name"`
		RedirectURIs []string       `json:"redirect_uris"`
		Settings     map[string]any `json:"settings"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	res, err := h.tenant.CreateApp(c.Request.Context(), tenantapp.CreateAppInput{
		AppName:      req.AppName,
		RedirectURIs: req.RedirectURIs,
		Settings:     req.Settings,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, appWithSecret(res.App, res.AppSecret))
}

func (h *Handlers) ListApps(c *gin.Context) {
	includeDisabled := c.Query("include_disabled") == "true" || c.Query("include_disabled") == "1"
	apps, err := h.tenant.ListApps(c.Request.Context(), tenantdomain.ListAppsFilter{IncludeDisabled: includeDisabled})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"apps": apps})
}

func (h *Handlers) GetApp(c *gin.Context) {
	app, err := h.tenant.GetApp(c.Request.Context(), c.Param("app_id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, app)
}

func (h *Handlers) UpdateApp(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	var typed struct {
		AppName      *string                 `json:"app_name"`
		RedirectURIs []string                `json:"redirect_uris"`
		Settings     map[string]any          `json:"settings"`
		Status       *tenantdomain.AppStatus `json:"status"`
	}
	if err := json.Unmarshal(body, &typed); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	in := tenantapp.UpdateAppInput{
		AppName: typed.AppName,
		Status:  typed.Status,
	}
	if _, ok := raw["redirect_uris"]; ok {
		in.SetURIs = true
		in.RedirectURIs = typed.RedirectURIs
	}
	if _, ok := raw["settings"]; ok {
		in.SetSettings = true
		in.Settings = typed.Settings
	}
	app, err := h.tenant.UpdateApp(c.Request.Context(), c.Param("app_id"), in)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, app)
}

func (h *Handlers) RotateSecret(c *gin.Context) {
	res, err := h.tenant.RotateAppSecret(c.Request.Context(), c.Param("app_id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, appWithSecret(res.App, res.AppSecret))
}

func (h *Handlers) DisableApp(c *gin.Context) {
	if err := h.tenant.DisableApp(c.Request.Context(), c.Param("app_id")); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"disabled": true})
}

func (h *Handlers) GetAuthConfig(c *gin.Context) {
	cfg, err := h.auth.GetAuthConfig(c.Request.Context(), c.Param("app_id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, cfg)
}

func (h *Handlers) PutAuthConfig(c *gin.Context) {
	var req authapp.UpdateAuthConfigInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	res, err := h.auth.UpdateAuthConfig(c.Request.Context(), c.Param("app_id"), req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *Handlers) GetBillingConfig(c *gin.Context) {
	cfg, err := h.billing.Get(c.Request.Context(), c.Param("app_id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, cfg)
}

func (h *Handlers) PutBillingConfig(c *gin.Context) {
	var req billing.Config
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	cfg, err := h.billing.Put(c.Request.Context(), c.Param("app_id"), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, cfg)
}

func (h *Handlers) GetOpsConfig(c *gin.Context) {
	cfg, err := h.ops.Get(c.Request.Context(), c.Param("app_id"))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, cfg)
}

func (h *Handlers) PutOpsConfig(c *gin.Context) {
	var req ops.Config
	_ = c.ShouldBindJSON(&req)
	cfg, err := h.ops.Put(c.Request.Context(), c.Param("app_id"), &req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, cfg)
}

func appWithSecret(app *tenantdomain.App, secret string) gin.H {
	return gin.H{
		"app_id":        app.AppID,
		"app_secret":    secret,
		"app_name":      app.AppName,
		"redirect_uris": app.RedirectURIs,
		"status":        app.Status,
		"settings":      app.Settings,
		"created_at":    app.CreatedAt,
		"updated_at":    app.UpdatedAt,
	}
}

// RequireAdmin ensures Bearer user is in ADMIN_APP_ID + ADMIN_EMAILS (fail-closed).
func RequireAdmin(authSvc *authapp.Service, adminAppID string, adminEmails []string) gin.HandlerFunc {
	emailSet := make(map[string]struct{}, len(adminEmails))
	for _, e := range adminEmails {
		emailSet[strings.ToLower(strings.TrimSpace(e))] = struct{}{}
	}
	return func(c *gin.Context) {
		if len(emailSet) == 0 {
			response.Fail(c, apperr.Forbidden(""))
			c.Abort()
			return
		}
		if !authpres.AuthenticateBearer(authSvc, c) {
			c.Abort()
			return
		}
		v, _ := c.Get(authpres.ContextUserKey)
		user, ok := v.(*domain.User)
		if !ok || user == nil {
			response.Fail(c, apperr.Forbidden(""))
			c.Abort()
			return
		}
		if user.AppID != adminAppID {
			response.Fail(c, apperr.Forbidden(""))
			c.Abort()
			return
		}
		if _, ok := emailSet[strings.ToLower(user.Email)]; !ok {
			response.Fail(c, apperr.Forbidden(""))
			c.Abort()
			return
		}
		c.Next()
	}
}
