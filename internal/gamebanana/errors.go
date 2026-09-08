package gamebanana

import "errors"

const (
	errCodeAuthRequired         = "GAMEBANANA_AUTH_REQUIRED"
	errCodeAuthFailed           = "GAMEBANANA_AUTH_FAILED"
	errCodeLoginCancelled       = "GAMEBANANA_LOGIN_CANCELLED"
	errCodeAutoLoginUnsupported = "GAMEBANANA_AUTO_LOGIN_UNSUPPORTED"
	errCodeServerUnreachable    = "GAMEBANANA_SERVER_UNREACHABLE"
	errCodeInvalidRMC           = "GAMEBANANA_INVALID_RMC"
	errCodeLoginInitFailed      = "GAMEBANANA_LOGIN_INIT_FAILED"
	errCodeAuthCheckFailed      = "GAMEBANANA_AUTH_CHECK_FAILED"
)

var (
	ErrAuthRequired         = errors.New(errCodeAuthRequired)
	ErrAuthFailed           = errors.New(errCodeAuthFailed)
	ErrLoginCancelled       = errors.New(errCodeLoginCancelled)
	ErrAutoLoginUnsupported = errors.New(errCodeAutoLoginUnsupported)
	ErrServerUnreachable    = errors.New(errCodeServerUnreachable)
	ErrInvalidRMC           = errors.New(errCodeInvalidRMC)
	ErrLoginInitFailed      = errors.New(errCodeLoginInitFailed)
	ErrAuthCheckFailed      = errors.New(errCodeAuthCheckFailed)
)
