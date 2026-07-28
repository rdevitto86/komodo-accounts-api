package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSendAccountError(t *testing.T) {
	t.Parallel()
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
			name:       "ErrEmailAlreadyExists -> 409",
			err:        ErrEmailAlreadyExists,
			wantStatus: http.StatusConflict,
		},
		{
			name:       "ErrPasskeySignCountRegression -> 409",
			err:        ErrPasskeySignCountRegression,
			wantStatus: http.StatusConflict,
		},
		{
			name:       "ErrInvalidUnsubscribeToken -> 400",
			err:        ErrInvalidUnsubscribeToken,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "ErrInvalidUnsubscribeChannel -> 400",
			err:        ErrInvalidUnsubscribeChannel,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "ErrInvalidCommunicationChannel -> 400",
			err:        ErrInvalidCommunicationChannel,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "ErrInvalidInput -> 400",
			err:        ErrInvalidInput,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "ErrVersionConflict -> 409",
			err:        ErrVersionConflict,
			wantStatus: http.StatusConflict,
		},
		{
			name:       "wrapped sentinel -> mapped status",
			err:        fmt.Errorf("account_id is required: %w", ErrInvalidInput),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown error -> 500",
			err:        errors.New("something unexpected"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rr := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, "/", nil)
			sendAccountError(rr, req, tc.err)
			assert.Equal(t, tc.wantStatus, rr.Code)

			dec := json.NewDecoder(rr.Body)
			var first map[string]any
			require.NoError(t, dec.Decode(&first))
			require.ErrorIs(t, dec.Decode(new(map[string]any)), io.EOF, "response must contain exactly one JSON document")
		})
	}
}
