package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSendAccountError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{
			name:       "ErrNotFound -> 404",
			err:        ErrNotFound,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "ErrPasskeyAlreadyExists -> 409",
			err:        ErrPasskeyAlreadyExists,
			wantStatus: http.StatusConflict,
		},
		{
			name:       "ErrAlreadyExists -> 409",
			err:        ErrAlreadyExists,
			wantStatus: http.StatusConflict,
		},
		{
			name:       "ErrForbiddenNamespace -> 403",
			err:        ErrForbiddenNamespace,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "unknown error -> 500",
			err:        errors.New("something unexpected"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, "/", nil)
			sendAccountError(rr, req, tc.err)
			assert.Equal(t, tc.wantStatus, rr.Code)
		})
	}
}
