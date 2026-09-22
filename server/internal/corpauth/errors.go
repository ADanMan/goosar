package corpauth

import "errors"

var (
	ErrNotConfigured = errors.New("corpauth: provider is not configured")

	ErrUnavailable = errors.New("corpauth: directory is unavailable")

	ErrInvalidCredentials = errors.New("corpauth: invalid credentials")

	ErrNoEmail = errors.New("corpauth: assertion carries no email address")

	ErrTokenInvalid = errors.New("corpauth: id token failed verification")
)
