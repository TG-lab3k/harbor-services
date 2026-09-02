package response

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/okok/harbor-services/internal/platform/apperr"
)

const RequestIDKey = "request_id"

// Envelope is the standard API response shape aligned with wachi-auth.
type Envelope struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data"`
	RequestID string      `json:"request_id"`
}

func requestIDFrom(c *gin.Context) string {
	if v, ok := c.Get(RequestIDKey); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return c.GetHeader("X-Request-Id")
}

// OK writes a successful envelope with code 0.
func OK(c *gin.Context, data interface{}) {
	if data == nil {
		data = gin.H{}
	}
	c.JSON(http.StatusOK, Envelope{
		Code:      apperr.CodeOK,
		Message:   "ok",
		Data:      data,
		RequestID: requestIDFrom(c),
	})
}

// Fail writes an error envelope; extracts HarborError when present.
func Fail(c *gin.Context, err error) {
	he, ok := apperr.AsHarborError(err)
	if !ok {
		var harborErr *apperr.HarborError
		if errors.As(err, &harborErr) {
			he = harborErr
			ok = true
		}
	}
	if !ok || he == nil {
		he = apperr.Internal("internal error")
	}
	c.JSON(he.HTTPStatus, Envelope{
		Code:      he.Code,
		Message:   he.Message,
		Data:      gin.H{},
		RequestID: requestIDFrom(c),
	})
}
