// Package web serves the combined Email + SMS inbox user interface and the
// management API used by integration tests.
package web

import (
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/akaporn-katip/sms-email-ui/internal/config"
	"github.com/akaporn-katip/sms-email-ui/internal/store"
)

//go:embed assets
var assetsFS embed.FS

// Server serves the web UI and management API.
type Server struct {
	store  *store.Store
	cfg    config.Config
	logger *slog.Logger
	mux    *http.ServeMux
}

// New creates the web server. mockAPI is mounted under /api/v1/ so that a
// single base URL works for both the UI and the mock SMS API.
func New(st *store.Store, cfg config.Config, logger *slog.Logger, mockAPI http.Handler) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{store: st, cfg: cfg, logger: logger, mux: http.NewServeMux()}

	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic("web: cannot open embedded assets: " + err.Error())
	}
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(sub)))
	s.mux.HandleFunc("GET /{$}", s.handleIndex)

	// Mock SMS API, also reachable through the web port.
	if mockAPI != nil {
		s.mux.Handle("/api/v1/", mockAPI)
	}

	// Management API.
	s.mux.HandleFunc("GET /api/stats", s.handleStats)
	s.mux.HandleFunc("GET /api/emails", s.handleEmailList)
	s.mux.HandleFunc("DELETE /api/emails", s.handleEmailClear)
	s.mux.HandleFunc("GET /api/emails/{id}", s.handleEmailGet)
	s.mux.HandleFunc("DELETE /api/emails/{id}", s.handleEmailDelete)
	s.mux.HandleFunc("GET /api/emails/{id}/raw", s.handleEmailRaw)
	s.mux.HandleFunc("GET /api/emails/{id}/attachments/{index}", s.handleEmailAttachment)
	s.mux.HandleFunc("POST /api/emails/{id}/read", s.handleEmailMarkRead)

	s.mux.HandleFunc("GET /api/sms", s.handleSMSList)
	s.mux.HandleFunc("DELETE /api/sms", s.handleSMSClear)
	s.mux.HandleFunc("GET /api/sms/{id}", s.handleSMSGet)
	s.mux.HandleFunc("DELETE /api/sms/{id}", s.handleSMSDelete)

	s.mux.HandleFunc("GET /api/otps", s.handleOTPList)
	s.mux.HandleFunc("DELETE /api/otps", s.handleOTPClear)
	s.mux.HandleFunc("GET /api/otp/latest", s.handleOTPLatest)

	s.mux.HandleFunc("POST /api/credit", s.handleSetCredit)
	s.mux.HandleFunc("GET /api/health", s.handleHealth)

	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Handler returns the web handler.
func (s *Server) Handler() http.Handler { return s }

// ---------------------------------------------------------------------------
// Static
// ---------------------------------------------------------------------------

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	data, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

// ---------------------------------------------------------------------------
// Management API
// ---------------------------------------------------------------------------

type emailSummary struct {
	ID          string    `json:"id"`
	From        string    `json:"from"`
	To          []string  `json:"to"`
	Subject     string    `json:"subject"`
	Date        time.Time `json:"date"`
	Size        int64     `json:"size"`
	Read        bool      `json:"read"`
	HasHTML     bool      `json:"hasHtml"`
	Attachments int       `json:"attachments"`
	Preview     string    `json:"preview"`
}

func summarize(e *store.Email) emailSummary {
	preview := strings.TrimSpace(e.Text)
	if preview == "" && e.HTML != "" {
		preview = stripTags(e.HTML)
	}
	preview = strings.Join(strings.Fields(preview), " ")
	if len(preview) > 200 {
		preview = preview[:200] + "…"
	}
	return emailSummary{
		ID:          e.ID,
		From:        e.From,
		To:          e.To,
		Subject:     e.Subject,
		Date:        e.Date,
		Size:        e.Size,
		Read:        e.Read,
		HasHTML:     e.HTML != "",
		Attachments: len(e.Attachments),
		Preview:     preview,
	}
}

func (s *Server) handleEmailList(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	limit := intQuery(r, "limit", 0)

	emails := s.store.Emails()
	out := make([]emailSummary, 0, len(emails))
	for _, e := range emails {
		if query != "" && !matchesEmail(e, query) {
			continue
		}
		out = append(out, summarize(e))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"emails": out, "total": len(out)})
}

func matchesEmail(e *store.Email, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{
		e.Subject, e.From, strings.Join(e.To, " "), e.Text, stripTags(e.HTML),
	}, "\n"))
	return strings.Contains(haystack, query)
}

func (s *Server) handleEmailGet(w http.ResponseWriter, r *http.Request) {
	e, ok := s.store.Email(r.PathValue("id"))
	if !ok {
		notFound(w, s.logger, "email not found")
		return
	}
	writeJSON(w, s.logger, http.StatusOK, e)
}

func (s *Server) handleEmailRaw(w http.ResponseWriter, r *http.Request) {
	e, ok := s.store.Email(r.PathValue("id"))
	if !ok {
		notFound(w, s.logger, "email not found")
		return
	}
	w.Header().Set("Content-Type", "message/rfc822")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeFilename(e.ID)+`.eml"`)
	_, _ = w.Write(e.Raw)
}

func (s *Server) handleEmailAttachment(w http.ResponseWriter, r *http.Request) {
	e, ok := s.store.Email(r.PathValue("id"))
	if !ok {
		notFound(w, s.logger, "email not found")
		return
	}
	idx, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || idx < 0 || idx >= len(e.Attachments) {
		notFound(w, s.logger, "attachment not found")
		return
	}
	att := e.Attachments[idx]
	w.Header().Set("Content-Type", att.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeFilename(att.Filename)+`"`)
	_, _ = w.Write(att.Data)
}

func (s *Server) handleEmailMarkRead(w http.ResponseWriter, r *http.Request) {
	read := true
	if raw := r.URL.Query().Get("read"); raw != "" {
		if b, err := strconv.ParseBool(raw); err == nil {
			read = b
		}
	}
	if !s.store.MarkEmailRead(r.PathValue("id"), read) {
		notFound(w, s.logger, "email not found")
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"id": r.PathValue("id"), "read": read})
}

func (s *Server) handleEmailDelete(w http.ResponseWriter, r *http.Request) {
	if !s.store.DeleteEmail(r.PathValue("id")) {
		notFound(w, s.logger, "email not found")
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"deleted": 1})
}

func (s *Server) handleEmailClear(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"deleted": s.store.ClearEmails()})
}

func (s *Server) handleSMSList(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	limit := intQuery(r, "limit", 0)

	msgs := s.store.SMSList()
	out := make([]*store.SMS, 0, len(msgs))
	for _, m := range msgs {
		if kind != "" && m.Kind != kind {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(m.Message+" "+m.Recipient+" "+m.Sender), query) {
			continue
		}
		out = append(out, m)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"messages": out, "total": len(out)})
}

func (s *Server) handleSMSGet(w http.ResponseWriter, r *http.Request) {
	m, ok := s.store.SMSByID(r.PathValue("id"))
	if !ok {
		notFound(w, s.logger, "message not found")
		return
	}
	writeJSON(w, s.logger, http.StatusOK, m)
}

func (s *Server) handleSMSDelete(w http.ResponseWriter, r *http.Request) {
	if !s.store.DeleteSMS(r.PathValue("id")) {
		notFound(w, s.logger, "message not found")
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"deleted": 1})
}

func (s *Server) handleSMSClear(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"deleted": s.store.ClearSMS()})
}

func (s *Server) handleOTPList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"otps": s.store.OTPs()})
}

func (s *Server) handleOTPClear(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"deleted": s.store.ClearOTPs()})
}

// handleOTPLatest returns the most recent OTP for a phone number. This is the
// endpoint integration tests use to read a code the way a human would read it
// from the inbox.
func (s *Server) handleOTPLatest(w http.ResponseWriter, r *http.Request) {
	phone := r.URL.Query().Get("phone")
	if phone == "" {
		badRequest(w, s.logger, "phone query parameter is required")
		return
	}
	otp, ok := s.store.LatestOTPFor(phone)
	if !ok {
		notFound(w, s.logger, "no OTP for this phone number")
		return
	}
	writeJSON(w, s.logger, http.StatusOK, otp)
}

type creditRequest struct {
	Credit *int `json:"credit"`
	Add    *int `json:"add"`
}

func (s *Server) handleSetCredit(w http.ResponseWriter, r *http.Request) {
	var req creditRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || len(body) == 0 {
		badRequest(w, s.logger, "request body is required")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		badRequest(w, s.logger, "invalid JSON: "+err.Error())
		return
	}
	switch {
	case req.Credit != nil:
		s.store.SetCredit(*req.Credit)
	case req.Add != nil:
		s.store.AddCredit(*req.Add)
	default:
		badRequest(w, s.logger, "provide either credit or add")
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"sms_remaining": s.store.Credit()})
}

func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	today, thisMonth := s.store.Analytics()
	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"emails":        s.store.EmailCount(),
		"messages":      s.store.SMSCount(),
		"otps":          s.store.OTPCount(),
		"contacts":      s.store.ContactCount(),
		"sms_remaining": s.store.Credit(),
		"rateLimit":     s.store.RateLimitEnabled(),
		"today":         today,
		"thisMonth":     thisMonth,
		"account": map[string]any{
			"name":  s.cfg.Name,
			"email": s.cfg.Email,
		},
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.logger, http.StatusOK, map[string]any{"status": "ok"})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, logger *slog.Logger, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Error("failed to write response", "error", err)
	}
}

func notFound(w http.ResponseWriter, logger *slog.Logger, msg string) {
	writeJSON(w, logger, http.StatusNotFound, map[string]any{"error": msg})
}

func badRequest(w http.ResponseWriter, logger *slog.Logger, msg string) {
	writeJSON(w, logger, http.StatusBadRequest, map[string]any{"error": msg})
}

func intQuery(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func safeFilename(name string) string {
	if name == "" {
		return "file"
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '"', '\\', '/', '\n', '\r', 0:
			return '_'
		}
		return r
	}, name)
}

// stripTags removes HTML tags and collapses whitespace for previews.
func stripTags(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			sb.WriteRune(' ')
		case !inTag:
			sb.WriteRune(r)
		}
	}
	out := sb.String()
	for _, entity := range []struct{ from, to string }{
		{"&nbsp;", " "}, {"&amp;", "&"}, {"&lt;", "<"}, {"&gt;", ">"}, {"&quot;", `"`}, {"&#39;", "'"},
	} {
		out = strings.ReplaceAll(out, entity.from, entity.to)
	}
	return out
}
