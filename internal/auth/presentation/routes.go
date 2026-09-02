package presentation

import (
	"github.com/gin-gonic/gin"

	authapp "github.com/okok/harbor-services/internal/auth/application"
)

// RegisterRoutes mounts Auth routes on the gin engine.
func RegisterRoutes(r *gin.Engine, svc *authapp.Service) {
	h := NewHandlers(svc)
	bearer := BearerAuth(svc)

	r.GET("/.well-known/jwks.json", h.JWKS)

	v1 := r.Group("/api/v1")
	{
		auth := v1.Group("/auth")
		{
			auth.POST("/register", h.Register)
			auth.POST("/login", h.Login)
			auth.POST("/refresh", h.Refresh)
			auth.POST("/logout", bearer, h.Logout)
			auth.POST("/verify-email", h.VerifyEmail)
			auth.POST("/forgot-password", h.ForgotPassword)
			auth.POST("/reset-password", h.ResetPassword)
			auth.GET("/oauth/:provider/authorize", h.OAuthAuthorize)
			auth.POST("/oauth/:provider/callback", h.OAuthCallback)
			auth.POST("/oauth/:provider/link", bearer, h.OAuthLink)
			auth.DELETE("/oauth/:provider/unlink", bearer, h.OAuthUnlink)
		}

		v1.POST("/oauth/introspect", h.Introspect)

		user := v1.Group("/user", bearer)
		{
			user.GET("/me", h.GetMe)
			user.POST("/me", h.UpdateMe)
			user.POST("/me/password", h.ChangePassword)
			user.POST("/me/email", h.ChangeEmail)
			user.GET("/me/account-links", h.ListAccountLinks)
			user.DELETE("/me", h.DeleteAccount)
		}
	}
}
