package apperr

import "fmt"

// HarborError is the standard business error with API code and HTTP status.
type HarborError struct {
	Code       int    `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
}

func (e *HarborError) Error() string {
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

// Error codes aligned with tech_01 §8 / wachi-auth.
const (
	CodeOK = 0

	CodeValidation   = 1001
	CodeEmailExists  = 1002
	CodeBadCreds     = 1003
	CodeUnverified   = 1004
	CodeUserDisabled = 1005
	CodeLocked       = 1006
	CodeTokenInvalid = 1007
	CodeOAuthFailed  = 1008
	CodeOAuthLinked  = 1009
	CodeRateLimited  = 1010

	CodeAppNotFound           = 2001
	CodeInvalidAppSecret      = 2002
	CodeUnauthorized          = 2003
	CodeForbidden             = 2004
	CodeCannotUnlink          = 2005
	CodeRedirectURINotAllowed = 2006
	CodeProviderNotConfigured = 2007

	CodeInternal = 9999
)

func newErr(code, httpStatus int, message string) *HarborError {
	return &HarborError{Code: code, Message: message, HTTPStatus: httpStatus}
}

func Validation(message string) *HarborError {
	if message == "" {
		message = "validation failed"
	}
	return newErr(CodeValidation, 422, message)
}

func EmailExists(message string) *HarborError {
	if message == "" {
		message = "email already registered"
	}
	return newErr(CodeEmailExists, 409, message)
}

func BadCredentials(message string) *HarborError {
	if message == "" {
		message = "invalid credentials"
	}
	return newErr(CodeBadCreds, 401, message)
}

func Unverified(message string) *HarborError {
	if message == "" {
		message = "email not verified"
	}
	return newErr(CodeUnverified, 403, message)
}

func UserDisabled(message string) *HarborError {
	if message == "" {
		message = "user disabled"
	}
	return newErr(CodeUserDisabled, 403, message)
}

func Locked(message string) *HarborError {
	if message == "" {
		message = "account locked"
	}
	return newErr(CodeLocked, 429, message)
}

func TokenInvalid(message string) *HarborError {
	if message == "" {
		message = "token invalid or expired"
	}
	return newErr(CodeTokenInvalid, 401, message)
}

func OAuthFailed(message string) *HarborError {
	if message == "" {
		message = "oauth failed"
	}
	return newErr(CodeOAuthFailed, 401, message)
}

func OAuthLinked(message string) *HarborError {
	if message == "" {
		message = "oauth already linked to another user"
	}
	return newErr(CodeOAuthLinked, 409, message)
}

func RateLimited(message string) *HarborError {
	if message == "" {
		message = "rate limited"
	}
	return newErr(CodeRateLimited, 429, message)
}

func AppNotFound(message string) *HarborError {
	if message == "" {
		message = "app not found or disabled"
	}
	return newErr(CodeAppNotFound, 404, message)
}

func InvalidAppSecret(message string) *HarborError {
	if message == "" {
		message = "invalid app secret"
	}
	return newErr(CodeInvalidAppSecret, 401, message)
}

func Unauthorized(message string) *HarborError {
	if message == "" {
		message = "unauthorized"
	}
	return newErr(CodeUnauthorized, 401, message)
}

func Forbidden(message string) *HarborError {
	if message == "" {
		message = "forbidden"
	}
	return newErr(CodeForbidden, 403, message)
}

func CannotUnlink(message string) *HarborError {
	if message == "" {
		message = "cannot unlink the only login method"
	}
	return newErr(CodeCannotUnlink, 400, message)
}

func RedirectURINotAllowed(message string) *HarborError {
	if message == "" {
		message = "redirect_uri not allowed"
	}
	return newErr(CodeRedirectURINotAllowed, 400, message)
}

func ProviderNotConfigured(message string) *HarborError {
	if message == "" {
		message = "provider not configured"
	}
	return newErr(CodeProviderNotConfigured, 400, message)
}

func Internal(message string) *HarborError {
	if message == "" {
		message = "internal error"
	}
	return newErr(CodeInternal, 500, message)
}

// AsHarborError returns *HarborError if err wraps or is one.
func AsHarborError(err error) (*HarborError, bool) {
	if err == nil {
		return nil, false
	}
	if he, ok := err.(*HarborError); ok {
		return he, true
	}
	return nil, false
}
