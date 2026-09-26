// Package smsapi implements a local mock of an SMS provider REST API v1.
//
// Each endpoint below is implemented with matching request and response shapes,
// but messages are recorded in memory instead of being handed to a real SMS
// gateway.
package smsapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/akaporn-katip/sms-email-ui/internal/config"
	"github.com/akaporn-katip/sms-email-ui/internal/store"
)

// Default rate limits.
const (
	limitSMSSend        = 10
	limitSMSBatch       = 5
	limitContactsImport = 5
	limitOTPSend        = 3
	limitOTPVerify      = 10
	limitGeneral        = 60
)

// Server is the mock SMS API.
type Server struct {
	store  *store.Store
	cfg    config.Config
	logger *slog.Logger
	mux    *http.ServeMux
}

// New creates the mock API handler.
func New(st *store.Store, cfg config.Config, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{store: st, cfg: cfg, logger: logger, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)

	// SMS
	s.mux.HandleFunc("POST /api/v1/sms/send", s.auth(s.handleSMSSend))
	s.mux.HandleFunc("POST /api/v1/sms/batch", s.auth(s.handleSMSBatch))
	s.mux.HandleFunc("GET /api/v1/sms/status", s.auth(s.handleSMSStatus))
	s.mux.HandleFunc("POST /api/v1/sms/scheduled", s.auth(s.handleSMSScheduled))

	// OTP
	s.mux.HandleFunc("POST /api/v1/otp/send", s.auth(s.handleOTPSend))
	s.mux.HandleFunc("POST /api/v1/otp/verify", s.auth(s.handleOTPVerify))

	// Contacts
	s.mux.HandleFunc("GET /api/v1/contacts", s.auth(s.handleContactsList))
	s.mux.HandleFunc("POST /api/v1/contacts", s.auth(s.handleContactsCreate))
	s.mux.HandleFunc("POST /api/v1/contacts/import", s.auth(s.handleContactsImport))

	// Account
	s.mux.HandleFunc("GET /api/v1/balance", s.auth(s.handleBalance))
	s.mux.HandleFunc("GET /api/v1/analytics", s.auth(s.handleAnalytics))
	s.mux.HandleFunc("POST /api/v1/api-keys", s.auth(s.handleCreateAPIKey))
	s.mux.HandleFunc("GET /api/v1/senders", s.auth(s.handleSenders))
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Handler returns the mock API wrapped with CORS handling.
func (s *Server) Handler() http.Handler {
	return cors(s)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// Middleware and helpers
// ---------------------------------------------------------------------------

// auth enforces bearer-token authentication. By default any
// token starting with "sk_" is accepted so that local SDKs work unchanged.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.limit("general", limitGeneral, time.Minute) {
			s.tooManyRequests(w)
			return
		}
		if !s.authorized(r) {
			writeJSON(w, s.logger, http.StatusUnauthorized, map[string]any{
				"error": "API Key ไม่ถูกต้อง",
				"code":  "UNAUTHORIZED",
			})
			return
		}
		next(w, r)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return false
	}
	token := header[len(prefix):]
	if s.cfg.APIKey != "" {
		return token == s.cfg.APIKey
	}
	return len(token) > 3 && token[:3] == "sk_"
}

func (s *Server) limit(key string, n int, window time.Duration) bool {
	return s.store.AllowRate(key, n, window)
}

func (s *Server) tooManyRequests(w http.ResponseWriter) {
	writeJSON(w, s.logger, http.StatusTooManyRequests, map[string]any{
		"error": "เกินขีดจำกัดการเรียกใช้งาน กรุณาลองใหม่ภายหลัง",
		"code":  "TOO_MANY_REQUESTS",
	})
}

func (s *Server) badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, s.logger, http.StatusBadRequest, map[string]any{
		"error": msg,
		"code":  "BAD_REQUEST",
	})
}

func (s *Server) insufficientCredit(w http.ResponseWriter) {
	writeJSON(w, s.logger, http.StatusPaymentRequired, map[string]any{
		"error": "SMS ไม่เพียงพอ",
		"code":  "INSUFFICIENT_SMS",
	})
}

// decode reads a JSON request body into v, writing a 400 response and
// returning false when the body is not valid JSON.
func (s *Server) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		s.badRequest(w, "ไม่สามารถอ่านข้อมูลที่ส่งมาได้")
		return false
	}
	if len(body) == 0 {
		s.badRequest(w, "ข้อมูลไม่ถูกต้อง: ไม่มี request body")
		return false
	}
	// Unknown fields are ignored on purpose: real gateways tolerate extra
	// parameters, and SDKs sometimes send optional ones.
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(v); err != nil {
		s.badRequest(w, "ข้อมูลไม่ถูกต้อง: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, logger *slog.Logger, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Error("failed to write response", "error", err)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"status": "ok"})
}

// ---------------------------------------------------------------------------
// Shared response mapping
// ---------------------------------------------------------------------------

func iso(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func isoPtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return iso(*t)
}

// statusResponse renders the GET /api/v1/sms/status payload.
func statusResponse(msg *store.SMS) map[string]any {
	recipient := msg.RawRecipient
	if recipient == "" {
		recipient = msg.Recipient
	}
	return map[string]any{
		"id":             msg.ID,
		"recipient":      recipient,
		"status":         msg.Status,
		"statusDetail":   msg.StatusDetail,
		"detail":         msg.Detail,
		"detailCategory": msg.DetailCategory,
		"senderName":     msg.Sender,
		"creditCost":     msg.CreditCost,
		"sentAt":         isoPtr(msg.SentAt),
		"deliveredAt":    isoPtr(msg.DeliveredAt),
	}
}
