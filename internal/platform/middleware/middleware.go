package middleware

import (
	"log"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/okok/harbor-services/internal/platform/response"
	"github.com/okok/harbor-services/internal/shared/idgen"
)

// RequestID injects a request id into context and response header.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.GetHeader("X-Request-Id")
		if rid == "" {
			rid = "req_" + idgen.RandomURLSafe(12)
		}
		c.Set(response.RequestIDKey, rid)
		c.Header("X-Request-Id", rid)
		c.Next()
	}
}

// Recover catches panics and returns a 500 envelope.
func Recover() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("panic recovered: %v\n%s", r, debug.Stack())
				c.AbortWithStatusJSON(http.StatusInternalServerError, response.Envelope{
					Code:      9999,
					Message:   "internal error",
					Data:      gin.H{},
					RequestID: c.GetString(response.RequestIDKey),
				})
			}
		}()
		c.Next()
	}
}

// CORS is a simple permissive CORS middleware for development.
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Request-Id")
		c.Header("Access-Control-Expose-Headers", "X-Request-Id")
		c.Header("Access-Control-Max-Age", "86400")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// AccessLog logs method, path, status and latency (optional helper).
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Printf("%s %s %d %s rid=%s",
			c.Request.Method,
			c.Request.URL.Path,
			c.Writer.Status(),
			time.Since(start),
			c.GetString(response.RequestIDKey),
		)
	}
}
