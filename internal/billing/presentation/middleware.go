package presentation

import (
	"encoding/base64"
	"io"
	"strings"

	"github.com/gin-gonic/gin"

	authapp "github.com/okok/harbor-services/internal/auth/application"
	authdomain "github.com/okok/harbor-services/internal/auth/domain"
	authpres "github.com/okok/harbor-services/internal/auth/presentation"
	billingapp "github.com/okok/harbor-services/internal/billing/application"
	"github.com/okok/harbor-services/internal/platform/apperr"
	"github.com/okok/harbor-services/internal/platform/response"
	sharedjwt "github.com/okok/harbor-services/internal/shared/jwt"
)

const (
	ContextAppKey    = "billing_app"
	ContextAppIDKey  = "billing_app_id"
	ContextAuthMode  = "billing_auth_mode"
	AuthModeSecret   = "app_secret"
	AuthModeJWT      = "jwt"
)

// Handlers exposes Billing HTTP adapters.
type Handlers struct {
	svc  *billingapp.Service
	auth *authapp.Service
}

func NewHandlers(svc *billingapp.Service, auth *authapp.Service) *Handlers {
	return &Handlers{svc: svc, auth: auth}
}

func parseAppSecret(c *gin.Context) (appID, secret string, ok bool) {
	if id := c.GetHeader("X-App-Id"); id != "" {
		sec := c.GetHeader("X-App-Secret")
		if sec != "" {
			return id, sec, true
		}
	}
	authz := c.GetHeader("Authorization")
	if strings.HasPrefix(strings.ToLower(authz), "basic ") {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(authz[6:]))
		if err != nil {
			return "", "", false
		}
		parts := strings.SplitN(string(raw), ":", 2)
		if len(parts) != 2 {
			return "", "", false
		}
		return parts[0], parts[1], true
	}
	return "", "", false
}

// RequireAppSecret authenticates product server calls.
func RequireAppSecret(svc *billingapp.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		appID, secret, ok := parseAppSecret(c)
		if !ok {
			response.Fail(c, apperr.Unauthorized("app credentials required"))
			c.Abort()
			return
		}
		app, err := svc.VerifySecret(c.Request.Context(), appID, secret)
		if err != nil {
			response.Fail(c, err)
			c.Abort()
			return
		}
		c.Set(ContextAppKey, app)
		c.Set(ContextAppIDKey, app.AppID)
		c.Set(ContextAuthMode, AuthModeSecret)
		c.Next()
	}
}

// RequireAppSecretOrJWT allows App Secret or user Bearer JWT (for order reads).
func RequireAppSecretOrJWT(billingSvc *billingapp.Service, authSvc *authapp.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if appID, secret, ok := parseAppSecret(c); ok {
			app, err := billingSvc.VerifySecret(c.Request.Context(), appID, secret)
			if err != nil {
				response.Fail(c, err)
				c.Abort()
				return
			}
			c.Set(ContextAppKey, app)
			c.Set(ContextAppIDKey, app.AppID)
			c.Set(ContextAuthMode, AuthModeSecret)
			c.Next()
			return
		}
		if !authpres.AuthenticateBearer(authSvc, c) {
			c.Abort()
			return
		}
		claims, _ := c.Get(authpres.ContextClaimsKey)
		cl, ok := claims.(*sharedjwt.Claims)
		if !ok || cl == nil {
			response.Fail(c, apperr.TokenInvalid(""))
			c.Abort()
			return
		}
		c.Set(ContextAppIDKey, cl.AppID)
		c.Set(ContextAuthMode, AuthModeJWT)
		c.Next()
	}
}

func appIDFromCtx(c *gin.Context) string {
	v, _ := c.Get(ContextAppIDKey)
	s, _ := v.(string)
	return s
}

func authMode(c *gin.Context) string {
	v, _ := c.Get(ContextAuthMode)
	s, _ := v.(string)
	return s
}

func jwtUserID(c *gin.Context) string {
	if authMode(c) != AuthModeJWT {
		return ""
	}
	u, ok := c.Get(authpres.ContextUserKey)
	if !ok {
		return ""
	}
	user, ok := u.(*authdomain.User)
	if !ok || user == nil {
		return ""
	}
	return user.UserID
}

func readBody(c *gin.Context) ([]byte, error) {
	defer c.Request.Body.Close()
	return io.ReadAll(c.Request.Body)
}
