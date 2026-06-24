package models

import (
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
)

// 30xxx — komodo-user-api (see forge-sdk ranges.go)
type UserAPIErrors struct {
	NotFound           httpErr.ErrorCode
	AlreadyExists      httpErr.ErrorCode
	AccountLocked      httpErr.ErrorCode
	AccountSuspended   httpErr.ErrorCode
	EmailNotVerified   httpErr.ErrorCode
	PhoneNotVerified   httpErr.ErrorCode
	InvalidCredentials httpErr.ErrorCode
	PasswordExpired    httpErr.ErrorCode
	WeakPassword       httpErr.ErrorCode
	MFARequired        httpErr.ErrorCode
	InvalidMFACode     httpErr.ErrorCode
}

var Err = UserAPIErrors{
	NotFound:           httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 1), Status: http.StatusNotFound, Message: "User not found"},
	AlreadyExists:      httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 2), Status: http.StatusConflict, Message: "User already exists"},
	AccountLocked:      httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 3), Status: http.StatusForbidden, Message: "Account locked"},
	AccountSuspended:   httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 4), Status: http.StatusForbidden, Message: "Account suspended"},
	EmailNotVerified:   httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 5), Status: http.StatusForbidden, Message: "Email not verified"},
	PhoneNotVerified:   httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 6), Status: http.StatusForbidden, Message: "Phone not verified"},
	InvalidCredentials: httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 7), Status: http.StatusUnauthorized, Message: "Invalid credentials"},
	PasswordExpired:    httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 8), Status: http.StatusForbidden, Message: "Password expired"},
	WeakPassword:       httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 9), Status: http.StatusBadRequest, Message: "Weak password"},
	MFARequired:        httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 10), Status: http.StatusForbidden, Message: "MFA required"},
	InvalidMFACode:     httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 11), Status: http.StatusUnauthorized, Message: "Invalid MFA code"},
}
