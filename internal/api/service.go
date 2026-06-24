package api

import (
	"encoding/json"
	"net/http"

	"komodo-user-api/internal/db"

	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
)

type Service struct {
	repo *db.Repo
}

func NewService(repo *db.Repo) *Service {
	return &Service{repo: repo}
}

func writeJSON(wtr http.ResponseWriter, v any) {
	if err := json.NewEncoder(wtr).Encode(v); err != nil {
		logger.Error("failed to encode response body", err)
	}
}
