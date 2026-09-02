package presentation

import (
	"encoding/base64"
	"strings"

	"github.com/gin-gonic/gin"

	authapp "github.com/okok/harbor-services/internal/auth/application"
	"github.com/okok/harbor-services/internal/auth/domain"
	"github.com/okok/harbor-services/internal/platform/apperr"
	"github.com/okok/harbor-services/internal/platform/response"
)

const (
	ContextUserKey   = "auth_user"
	ContextClaimsKey = "auth_claims"
)

// Handlers exposes Auth HTTP adapters.
type Handlers struct {
	svc *authapp.Service
}

func NewHandlers(svc *authapp.Service) *Handlers {
	return &Handlers{svc: svc}
}

func (h *Handlers) Register(c *gin.Context) {
	var req struct {
		AppID    string `json:"app_id"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Nickname string `json:"nickname"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	res, err := h.svc.Register(c.Request.Context(), req.AppID, req.Email, req.Password, req.Nickname)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *Handlers) Login(c *gin.Context) {
	var req struct {
		AppID    string `json:"app_id"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	res, err := h.svc.Login(c.Request.Context(), req.AppID, req.Email, req.Password, c.ClientIP())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *Handlers) Refresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	res, err := h.svc.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *Handlers) Logout(c *gin.Context) {
	user := mustUser(c)
	if err := h.svc.Logout(c.Request.Context(), user); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{})
}

func (h *Handlers) VerifyEmail(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
		AppID string `json:"app_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	if err := h.svc.VerifyEmail(c.Request.Context(), req.Token, req.AppID); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"verified": true})
}

func (h *Handlers) ForgotPassword(c *gin.Context) {
	var req struct {
		AppID string `json:"app_id"`
		Email string `json:"email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	if err := h.svc.ForgotPassword(c.Request.Context(), req.AppID, req.Email); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{})
}

func (h *Handlers) ResetPassword(c *gin.Context) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	if err := h.svc.ResetPassword(c.Request.Context(), req.Token, req.NewPassword); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{})
}

func (h *Handlers) OAuthAuthorize(c *gin.Context) {
	provider := c.Param("provider")
	appID := c.Query("app_id")
	redirectURI := c.Query("redirect_uri")
	res, err := h.svc.OAuthAuthorize(c.Request.Context(), appID, provider, redirectURI)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *Handlers) OAuthCallback(c *gin.Context) {
	provider := c.Param("provider")
	var req struct {
		AppID       string `json:"app_id"`
		Code        string `json:"code"`
		IDToken     string `json:"id_token"`
		RedirectURI string `json:"redirect_uri"`
		State       string `json:"state"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	res, err := h.svc.OAuthCallback(c.Request.Context(), authapp.OAuthCallbackInput{
		AppID:       req.AppID,
		Provider:    provider,
		Code:        req.Code,
		IDToken:     req.IDToken,
		RedirectURI: req.RedirectURI,
		State:       req.State,
	})
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *Handlers) OAuthLink(c *gin.Context) {
	user := mustUser(c)
	provider := c.Param("provider")
	var req struct {
		Code        string `json:"code"`
		IDToken     string `json:"id_token"`
		RedirectURI string `json:"redirect_uri"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	if err := h.svc.OAuthLink(c.Request.Context(), user, provider, req.Code, req.IDToken, req.RedirectURI); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"linked": true})
}

func (h *Handlers) OAuthUnlink(c *gin.Context) {
	user := mustUser(c)
	provider := c.Param("provider")
	if err := h.svc.OAuthUnlink(c.Request.Context(), user, provider); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"unlinked": true})
}

func (h *Handlers) Introspect(c *gin.Context) {
	appID, secret, ok := parseAppSecret(c)
	if !ok {
		response.Fail(c, apperr.Unauthorized("app credentials required"))
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	res, err := h.svc.Introspect(c.Request.Context(), appID, secret, req.Token)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *Handlers) GetMe(c *gin.Context) {
	user := mustUser(c)
	res, err := h.svc.GetMe(c.Request.Context(), user)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *Handlers) UpdateMe(c *gin.Context) {
	user := mustUser(c)
	var req authapp.UpdateMeInput
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	res, err := h.svc.UpdateMe(c.Request.Context(), user, req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

func (h *Handlers) ChangePassword(c *gin.Context) {
	user := mustUser(c)
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	if err := h.svc.ChangePassword(c.Request.Context(), user, req.OldPassword, req.NewPassword); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{})
}

func (h *Handlers) ChangeEmail(c *gin.Context) {
	user := mustUser(c)
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, apperr.Validation("invalid request body"))
		return
	}
	if err := h.svc.ChangeEmail(c.Request.Context(), user, req.Email, req.Password); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{})
}

func (h *Handlers) ListAccountLinks(c *gin.Context) {
	user := mustUser(c)
	links, err := h.svc.ListAccountLinks(c.Request.Context(), user)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{"links": links})
}

func (h *Handlers) DeleteAccount(c *gin.Context) {
	user := mustUser(c)
	var req struct {
		Password string `json:"password"`
	}
	_ = c.ShouldBindJSON(&req)
	if err := h.svc.DeleteAccount(c.Request.Context(), user, req.Password); err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, gin.H{})
}

func (h *Handlers) JWKS(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(200, h.svc.JWKS())
}

// AuthenticateBearer validates the Authorization Bearer token and sets user/claims on context.
// It does not call c.Next(); callers that are gin middleware should Abort on false.
func AuthenticateBearer(svc *authapp.Service, c *gin.Context) bool {
	authz := c.GetHeader("Authorization")
	if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		response.Fail(c, apperr.Unauthorized(""))
		return false
	}
	token := strings.TrimSpace(authz[7:])
	user, claims, err := svc.LoadUserForAccess(c.Request.Context(), token)
	if err != nil {
		response.Fail(c, err)
		return false
	}
	c.Set(ContextUserKey, user)
	c.Set(ContextClaimsKey, claims)
	return true
}

// BearerAuth loads the current user from Authorization Bearer access token.
func BearerAuth(svc *authapp.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !AuthenticateBearer(svc, c) {
			c.Abort()
			return
		}
		c.Next()
	}
}

func mustUser(c *gin.Context) *domain.User {
	v, _ := c.Get(ContextUserKey)
	u, _ := v.(*domain.User)
	return u
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
