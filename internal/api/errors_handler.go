package api

import (
	"errors"
	"net/http"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"

	"komodo-user-api/internal/models"
)

func sendUserError(wtr http.ResponseWriter, req *http.Request, err error) {
	if errors.Is(err, ErrNotFound) {
		httpErr.SendError(wtr, req, httpErr.Global.NotFound)
		return
	}
	if errors.Is(err, ErrAlreadyExists) {
		httpErr.SendError(wtr, req, models.Err.AlreadyExists)
		return
	}
	httpErr.SendError(wtr, req, httpErr.Global.Internal)
}
