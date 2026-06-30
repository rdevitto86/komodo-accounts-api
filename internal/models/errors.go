package models

import (
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
)

type UserAPIErrors struct {
	NotFound                   httpErr.ErrorCode
	AlreadyExists              httpErr.ErrorCode
	AccountLocked              httpErr.ErrorCode
	AccountSuspended           httpErr.ErrorCode
	EmailNotVerified           httpErr.ErrorCode
	PhoneNotVerified           httpErr.ErrorCode
	InvalidCredentials         httpErr.ErrorCode
	PasswordExpired            httpErr.ErrorCode
	WeakPassword               httpErr.ErrorCode
	MFARequired                httpErr.ErrorCode
	InvalidMFACode             httpErr.ErrorCode
	PasskeyAlreadyExists       httpErr.ErrorCode
	ForbiddenNamespace         httpErr.ErrorCode
	PasskeySignCountRegression httpErr.ErrorCode
	InvalidUnsubscribeToken    httpErr.ErrorCode
	InvalidUnsubscribeChannel  httpErr.ErrorCode
	InvalidInput                httpErr.ErrorCode
	AccountNotPendingDeletion   httpErr.ErrorCode
	InvalidCommunicationChannel httpErr.ErrorCode
	VersionConflict             httpErr.ErrorCode
}

var Err = UserAPIErrors{
	NotFound:                   httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 1), Status: http.StatusNotFound, Message: "User not found"},
	AlreadyExists:              httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 2), Status: http.StatusConflict, Message: "User already exists"},
	AccountLocked:              httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 3), Status: http.StatusForbidden, Message: "Account locked"},
	AccountSuspended:           httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 4), Status: http.StatusForbidden, Message: "Account suspended"},
	EmailNotVerified:           httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 5), Status: http.StatusForbidden, Message: "Email not verified"},
	PhoneNotVerified:           httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 6), Status: http.StatusForbidden, Message: "Phone not verified"},
	InvalidCredentials:         httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 7), Status: http.StatusUnauthorized, Message: "Invalid credentials"},
	PasswordExpired:            httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 8), Status: http.StatusForbidden, Message: "Password expired"},
	WeakPassword:               httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 9), Status: http.StatusBadRequest, Message: "Weak password"},
	MFARequired:                httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 10), Status: http.StatusForbidden, Message: "MFA required"},
	InvalidMFACode:             httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 11), Status: http.StatusUnauthorized, Message: "Invalid MFA code"},
	PasskeyAlreadyExists:       httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 12), Status: http.StatusConflict, Message: "Passkey already exists"},
	ForbiddenNamespace:         httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 13), Status: http.StatusForbidden, Message: "Forbidden tag namespace"},
	PasskeySignCountRegression: httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 15), Status: http.StatusConflict, Message: "Passkey sign count regression rejected"},
	InvalidUnsubscribeToken:    httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 16), Status: http.StatusBadRequest, Message: "Invalid or expired unsubscribe token"},
	InvalidUnsubscribeChannel:  httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 17), Status: http.StatusBadRequest, Message: "Invalid unsubscribe channel"},
	InvalidInput:                httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 18), Status: http.StatusBadRequest, Message: "Invalid input"},
	AccountNotPendingDeletion:   httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 19), Status: http.StatusConflict, Message: "Account is not pending deletion or the restore window has expired"},
	InvalidCommunicationChannel: httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 20), Status: http.StatusBadRequest, Message: "invalid communication channel"},
	VersionConflict:             httpErr.ErrorCode{ID: httpErr.CodeID(httpErr.RangeUser, 21), Status: http.StatusConflict, Message: "version conflict"},
}
