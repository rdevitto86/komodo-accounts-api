package middleware

import (
	"net/http"

	ctxKeys "github.com/rdevitto86/komodo-forge-sdk-go/http/context"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

// Middleware that logs the scopes of a JWT bearer token for audit purposes.
func JWTScopeObservabilityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(wtr http.ResponseWriter, req *http.Request) {
		route := req.Pattern
		if route == "" {
			route = req.Method + " " + req.URL.Path
		}

		ctx := req.Context()
		logger.Info(
			"private request scopes observed",
			logger.AttrRequestID(ctxKeys.GetRequestID(ctx)),
			logger.AttrUserID(ctxKeys.GetUserID(ctx)),
			logger.Attr("route", route),
			logger.Attr("plane", ctxKeys.GetPlane(ctx)),
			logger.Attr("scopes", ctxKeys.GetScopes(ctx)),
		)

		next.ServeHTTP(wtr, req)
	})
}
