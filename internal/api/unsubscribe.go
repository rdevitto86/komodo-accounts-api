package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	httpErr "github.com/rdevitto86/komodo-forge-sdk-go/api/errors"
	httpReq "github.com/rdevitto86/komodo-forge-sdk-go/api/request"
	logger "github.com/rdevitto86/komodo-forge-sdk-go/logging/runtime"
	"github.com/segmentio/ksuid"

	"komodo-accounts-api/internal/db"
	"komodo-accounts-api/internal/models"
)

type unsubPayload struct {
	AccountID string `json:"account_id"`
	Channel   string `json:"channel"`
	Exp       int64  `json:"exp"`
	JTI       string `json:"jti"`
}

// Route handler that verifies an unsubscribe token and records the unsubscribe event
func (s *Service) UnsubscribeHandler(wtr http.ResponseWriter, req *http.Request) {
	var input models.UnsubscribeRequest
	if err := decodeStrict(req, &input); err != nil || input.Token == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	ip := httpReq.GetClientKey(req)
	ua := req.Header.Get("User-Agent")

	if err := s.VerifyAndRecordUnsubscribe(req.Context(), input.Token, ip, ua); err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	wtr.WriteHeader(http.StatusNoContent)
}

// Route handler that mints an unsubscribe token for a given account and channel
func (s *Service) MintUnsubscribeTokenHandler(wtr http.ResponseWriter, req *http.Request) {
	accountID := req.PathValue("id")
	if accountID == "" {
		logger.Warn("rejected unauthorized request", nil)
		httpErr.SendError(wtr, req, httpErr.Global.Unauthorized)
		return
	}

	var input models.MintUnsubscribeTokenRequest
	if err := decodeStrict(req, &input); err != nil || input.Channel == "" {
		httpErr.SendError(wtr, req, httpErr.Global.BadRequest)
		return
	}

	token, err := s.MintUnsubscribeToken(req.Context(), accountID, input.Channel)
	if err != nil {
		sendAccountError(wtr, req, err)
		return
	}

	wtr.Header().Set("Content-Type", "application/json")
	wtr.WriteHeader(http.StatusCreated)
	writeJSON(wtr, models.MintUnsubscribeTokenResponse{Token: &token})
}

// Confirms and modifies an account subscription preferences in the DB.
func (s *Service) VerifyAndRecordUnsubscribe(ctx context.Context, token, ipAddr, userAgent string) error {
	if len(s.unsubscribeKey) == 0 {
		return errors.New("unsubscribe key not configured")
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) < 32 {
		return fmt.Errorf("invalid unsubscribe token: %w", ErrInvalidUnsubscribeToken)
	}
	payloadBytes := raw[:len(raw)-32]
	sig := raw[len(raw)-32:]

	mac := hmac.New(sha256.New, s.unsubscribeKey)
	mac.Write(payloadBytes)
	if !hmac.Equal(mac.Sum(nil), sig) {
		return fmt.Errorf("invalid unsubscribe token signature: %w", ErrInvalidUnsubscribeToken)
	}

	p, err := parseUnsubToken(payloadBytes)
	if err != nil {
		return err
	}
	if len(ipAddr) > 256 {
		ipAddr = ipAddr[:256]
	}
	if len(userAgent) > 256 {
		userAgent = userAgent[:256]
	}

	existing, err := s.repo.GetLatestConsent(ctx, p.AccountID, p.Channel)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("failed to check consent log for replay: %w", err)
	}
	if existing != nil && existing.SourceRef != nil && *existing.SourceRef == p.JTI {
		return nil
	}

	entry := &models.ConsentLog{
		Channel:   models.ConsentLogChannel(p.Channel),
		Action:    models.ConsentLogAction("opt_out"),
		Source:    "unsubscribe_link",
		SourceRef: &p.JTI,
		IpAddress: &ipAddr,
		UserAgent: &userAgent,
	}
	if err := s.repo.AppendConsentLog(ctx, p.AccountID, entry); err != nil {
		return fmt.Errorf("failed to record unsubscribe: %w", err)
	}
	return nil
}

// Creates a new unsubscribe token for a given account.
func (s *Service) MintUnsubscribeToken(ctx context.Context, accountID, channel string) (string, error) {
	if len(s.unsubscribeKey) == 0 {
		return "", errors.New("unsubscribe key not configured")
	}
	if !models.ValidCommunicationChannels[channel] {
		return "", fmt.Errorf("invalid unsubscribe channel %q: %w", channel, ErrInvalidUnsubscribeChannel)
	}
	if _, err := s.repo.GetAccount(ctx, accountID); err != nil {
		if isNotFound(err) {
			return "", fmt.Errorf("account not found: %w", ErrNotFound)
		}
		return "", fmt.Errorf("failed to verify account for token mint: %w", err)
	}

	payload, err := json.Marshal(unsubPayload{
		AccountID: accountID,
		Channel:   channel,
		Exp:       time.Now().Add(s.unsubTokenTTL).Unix(),
		JTI:       ksuid.New().String(),
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal unsubscribe payload: %w", err)
	}

	mac := hmac.New(sha256.New, s.unsubscribeKey)
	mac.Write(payload)
	sig := mac.Sum(nil)
	token := base64.RawURLEncoding.EncodeToString(append(payload, sig...))
	return token, nil
}

// Helper that parses an unsubscription token.
func parseUnsubToken(payloadBytes []byte) (*unsubPayload, error) {
	var p unsubPayload
	if err := json.Unmarshal(payloadBytes, &p); err != nil {
		return nil, fmt.Errorf("failed to parse unsubscribe payload: %w", ErrInvalidUnsubscribeToken)
	}
	if p.JTI == "" {
		return nil, fmt.Errorf("missing jti in unsubscribe token: %w", ErrInvalidUnsubscribeToken)
	}
	if len(p.JTI) > 256 {
		p.JTI = p.JTI[:256]
	}
	if time.Now().Unix() > p.Exp {
		return nil, fmt.Errorf("unsubscribe token expired: %w", ErrInvalidUnsubscribeToken)
	}
	if !models.ValidCommunicationChannels[p.Channel] {
		return nil, fmt.Errorf("invalid unsubscribe channel %q: %w", p.Channel, ErrInvalidUnsubscribeChannel)
	}
	return &p, nil
}
