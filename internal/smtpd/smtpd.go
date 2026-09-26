// Package smtpd implements the fake SMTP server that captures outgoing email.
//
// It accepts every message, with or without authentication, and never relays
// anything: captured mail is written to the in-memory store and shown in the
// web UI.
package smtpd

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/mail"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	// Registering the charset package enables decoding of non UTF-8 bodies.
	_ "github.com/emersion/go-message/charset"
	gomail "github.com/emersion/go-message/mail"

	"github.com/akaporn-katip/sms-email-ui/internal/store"
)

// MaxMessageBytes is the largest message the catcher accepts.
const MaxMessageBytes = 50 << 20

// Backend implements smtp.Backend.
type Backend struct {
	store  *store.Store
	logger *slog.Logger
}

// NewBackend creates a backend writing captured mail into st.
func NewBackend(st *store.Store, logger *slog.Logger) *Backend {
	if logger == nil {
		logger = slog.Default()
	}
	return &Backend{store: st, logger: logger}
}

// NewSession implements smtp.Backend.
func (b *Backend) NewSession(_ *smtp.Conn) (smtp.Session, error) {
	return &session{backend: b}, nil
}

// NewServer builds a configured SMTP server catching mail into st.
func NewServer(addr string, st *store.Store, logger *slog.Logger) *smtp.Server {
	srv := smtp.NewServer(NewBackend(st, logger))
	srv.Addr = addr
	srv.Domain = "localhost"
	srv.ReadTimeout = 60 * time.Second
	srv.WriteTimeout = 60 * time.Second
	srv.MaxMessageBytes = MaxMessageBytes
	srv.MaxRecipients = 200
	// Local clients rarely offer STARTTLS, so allow AUTH in the clear. Every
	// credential is accepted anyway.
	srv.AllowInsecureAuth = true
	srv.ErrorLog = slogAdapter{logger}
	return srv
}

type session struct {
	backend *Backend
	from    string
	rcpts   []string
}

func (s *session) Mail(from string, _ *smtp.MailOptions) error {
	s.Reset()
	s.from = from
	return nil
}

func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	s.rcpts = append(s.rcpts, to)
	return nil
}

func (s *session) Reset() {
	s.from = ""
	s.rcpts = nil
}

func (s *session) Logout() error {
	s.Reset()
	return nil
}

func (s *session) Data(r io.Reader) error {
	raw, err := io.ReadAll(io.LimitReader(r, MaxMessageBytes+1))
	if err != nil {
		return err
	}
	email := Parse(raw)
	email.From = firstNonEmpty(email.From, s.from)
	if len(email.To) == 0 {
		email.To = append([]string(nil), s.rcpts...)
	}
	if email.Date.IsZero() {
		email.Date = time.Now()
	}
	s.backend.store.AddEmail(email)
	s.backend.logger.Info("captured email",
		"id", email.ID,
		"from", email.From,
		"to", strings.Join(email.To, ", "),
		"subject", email.Subject,
		"size", email.Size,
	)
	s.Reset()
	return nil
}

// AuthMechanisms implements smtp.AuthSession.
func (s *session) AuthMechanisms() []string { return []string{sasl.Plain, sasl.Anonymous} }

// Auth implements smtp.AuthSession. Every credential is accepted because this
// server only ever receives mail for inspection.
func (s *session) Auth(mech string) (sasl.Server, error) {
	switch mech {
	case sasl.Plain:
		return sasl.NewPlainServer(func(_, _, _ string) error { return nil }), nil
	case sasl.Anonymous:
		return sasl.NewAnonymousServer(func(_ string) error { return nil }), nil
	default:
		return nil, smtp.ErrAuthUnknownMechanism
	}
}

// Parse decodes a raw RFC 5322 message into a store.Email.
func Parse(raw []byte) *store.Email {
	email := &store.Email{
		ID:      store.NewMessageID(),
		Size:    int64(len(raw)),
		Headers: map[string][]string{},
		Raw:     raw,
	}

	if msg, err := mail.ReadMessage(bytes.NewReader(raw)); err == nil {
		email.From = headerAddress(msg.Header.Get("From"))
		email.To = headerAddressList(msg.Header.Get("To"))
		email.Cc = headerAddressList(msg.Header.Get("Cc"))
		email.Subject = decodeHeader(msg.Header.Get("Subject"))
		if d, err := msg.Header.Date(); err == nil {
			email.Date = d
		}
		for k, vs := range msg.Header {
			email.Headers[k] = append([]string(nil), vs...)
		}
	}

	mr, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		email.Text = string(raw)
		return email
	}

	if subject, err := mr.Header.Subject(); err == nil && subject != "" {
		email.Subject = subject
	}
	if from, err := mr.Header.AddressList("From"); err == nil && len(from) > 0 {
		email.From = from[0].Address
	}
	if date, err := mr.Header.Date(); err == nil {
		email.Date = date
	}

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch h := part.Header.(type) {
		case *gomail.InlineHeader:
			body, _ := io.ReadAll(part.Body)
			contentType, _, _ := h.ContentType()
			if strings.HasPrefix(strings.ToLower(contentType), "text/html") {
				email.HTML += string(body)
			} else {
				email.Text += string(body)
			}
		case *gomail.AttachmentHeader:
			body, _ := io.ReadAll(part.Body)
			contentType, _, _ := h.ContentType()
			filename, _ := h.Filename()
			email.Attachments = append(email.Attachments, store.Attachment{
				Filename:    filename,
				ContentType: contentType,
				Size:        len(body),
				Data:        body,
			})
		}
	}
	return email
}

func headerAddress(value string) string {
	if value == "" {
		return ""
	}
	if addr, err := mail.ParseAddress(value); err == nil {
		return addr.Address
	}
	return strings.TrimSpace(value)
}

func headerAddressList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	addrs, err := mail.ParseAddressList(value)
	if err != nil {
		parts := strings.Split(value, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.Address)
	}
	return out
}

// decodeHeader decodes RFC 2047 encoded words when present.
func decodeHeader(value string) string {
	dec := new(mime.WordDecoder)
	s, err := dec.DecodeHeader(value)
	if err != nil {
		return value
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// slogAdapter adapts *slog.Logger to the smtp.Logger interface.
type slogAdapter struct {
	logger *slog.Logger
}

func (a slogAdapter) Printf(format string, v ...any) {
	a.logger.Debug("smtp", "message", fmt.Sprintf(format, v...))
}

func (a slogAdapter) Println(v ...any) {
	a.logger.Debug("smtp", "message", fmt.Sprintln(v...))
}
