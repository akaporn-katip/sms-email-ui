// Package store is the in-memory message store shared by the SMTP catcher, the
// mock SMS API and the web UI. Nothing is persisted: restarting the
// process clears everything, exactly like Mailpit's default mode.
package store

import (
	"errors"
	"sync"
	"time"
)

// Message lifecycle statuses, exposed as the raw `status` value.
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusSent       = "sent"
	StatusDelivered  = "delivered"
	StatusFailed     = "failed"
	StatusCancelled  = "cancelled"
)

// Human-facing statuses exposed as `statusDetail`.
const (
	StatusDetailUnknown        = ""
	StatusDetailSent           = "sent"
	StatusDetailDelivered      = "delivered"
	StatusDetailDeliveryFailed = "delivery_failed"
	StatusDetailSendError      = "send_error"
)

// Failure details shown to end users.
const (
	DetailSubscriberUnreachable   = "ปลายทางไม่พร้อมรับข้อความในขณะนี้"
	CategorySubscriberUnreachable = "SUBSCRIBER_UNREACHABLE"

	DetailInvalidRecipient   = "เบอร์ปลายทางไม่ถูกต้องหรือไม่พร้อมใช้งาน"
	CategoryInvalidRecipient = "INVALID_RECIPIENT"
)

// Message kinds recorded in the SMS log.
const (
	KindSMS       = "sms"
	KindBatch     = "batch"
	KindOTP       = "otp"
	KindScheduled = "scheduled"
)

// Lifecycle timings: how long after a message is accepted it becomes
// processing, then sent, then delivered.
const (
	processingAfter = 1 * time.Second
	sentAfter       = 2 * time.Second
	deliveredAfter  = 4 * time.Second
)

// Errors returned by the store.
var (
	ErrNotFound        = errors.New("not found")
	ErrInsufficientSMS = errors.New("insufficient sms credit")
	ErrInvalidInput    = errors.New("invalid input")
)

// Config configures a Store.
type Config struct {
	// InitialCredit is the starting SMS balance. Defaults to 1500.
	InitialCredit int
	// AccountName and AccountEmail are reported by GET /api/v1/balance.
	AccountName  string
	AccountEmail string
	// RateLimitEnabled turns on the default rate limits.
	RateLimitEnabled bool
	// FailureNumbers lists recipient numbers that always fail delivery.
	// Values may be given in local or E.164 form. Defaults to ["0000000000"].
	FailureNumbers []string
}

// Attachment is a decoded email attachment.
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	Data        []byte `json:"-"`
}

// Email is a message captured by the SMTP server.
type Email struct {
	ID          string              `json:"id"`
	From        string              `json:"from"`
	To          []string            `json:"to"`
	Cc          []string            `json:"cc,omitempty"`
	Subject     string              `json:"subject"`
	Date        time.Time           `json:"date"`
	Text        string              `json:"text"`
	HTML        string              `json:"html"`
	Headers     map[string][]string `json:"headers"`
	Attachments []Attachment        `json:"attachments"`
	Size        int64               `json:"size"`
	Read        bool                `json:"read"`
	Raw         []byte              `json:"-"`
}

// SMS is a message recorded by the mock SMS API.
type SMS struct {
	ID             string     `json:"id"`
	Kind           string     `json:"kind"`
	Sender         string     `json:"sender"`
	Recipient      string     `json:"recipient"`
	Message        string     `json:"message"`
	Status         string     `json:"status"`
	StatusDetail   string     `json:"statusDetail"`
	Detail         *string    `json:"detail"`
	DetailCategory *string    `json:"detailCategory"`
	CreditCost     int        `json:"creditCost"`
	CreatedAt      time.Time  `json:"createdAt"`
	SentAt         *time.Time `json:"sentAt"`
	DeliveredAt    *time.Time `json:"deliveredAt"`
	ScheduledAt    *time.Time `json:"scheduledAt"`
	Ref            string     `json:"ref,omitempty"`
	OTPCode        string     `json:"otpCode,omitempty"`
	BatchID        string     `json:"batchId,omitempty"`

	// RawRecipient keeps the recipient exactly as the caller supplied it so
	// that status responses echo back the same format.
	RawRecipient string `json:"-"`
}

// OTP is a generated one-time password.
type OTP struct {
	ID          string     `json:"id"`
	Ref         string     `json:"ref"`
	Phone       string     `json:"phone"`
	PhoneRaw    string     `json:"phoneRaw"`
	Purpose     string     `json:"purpose"`
	Code        string     `json:"code"`
	Attempts    int        `json:"attempts"`
	MaxAttempts int        `json:"maxAttempts"`
	Verified    bool       `json:"verified"`
	Locked      bool       `json:"locked"`
	CreatedAt   time.Time  `json:"createdAt"`
	ExpiresAt   time.Time  `json:"expiresAt"`
	ConsumedAt  *time.Time `json:"consumedAt,omitempty"`
}

// Contact is an address-book entry from the contacts API.
type Contact struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Phone string `json:"phone"`
	Email string `json:"email,omitempty"`
	Tags  string `json:"tags,omitempty"`
}

// Sender is an approved sender name.
type Sender struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// APIKey is a generated API key. The secret is only returned at creation time.
type APIKey struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Key       string    `json:"key"`
	CreatedAt time.Time `json:"createdAt"`
}

// Stats holds message counters for a time range.
type Stats struct {
	Total     int `json:"total"`
	Delivered int `json:"delivered"`
	Failed    int `json:"failed"`
}

// Store is a concurrency-safe in-memory message store.
type Store struct {
	mu  sync.Mutex
	cfg Config

	now func() time.Time

	emails     map[string]*Email
	emailOrder []string

	sms      map[string]*SMS
	smsOrder []string

	otps     map[string]*OTP
	otpOrder []string

	contacts     map[string]*Contact
	contactOrder []string

	apiKeys []*APIKey
	senders []Sender

	credit  int
	limiter *RateLimiter
}

// New creates an empty store.
func New(cfg Config) *Store {
	if cfg.InitialCredit <= 0 {
		cfg.InitialCredit = 1500
	}
	if cfg.AccountName == "" {
		cfg.AccountName = "Local Developer"
	}
	if cfg.AccountEmail == "" {
		cfg.AccountEmail = "dev@example.com"
	}
	if len(cfg.FailureNumbers) == 0 {
		cfg.FailureNumbers = []string{"0000000000"}
	}
	normalized := make([]string, 0, len(cfg.FailureNumbers))
	for _, n := range cfg.FailureNumbers {
		if v := NormalizePhone(n); v != "" {
			normalized = append(normalized, v)
		}
	}
	cfg.FailureNumbers = normalized

	return &Store{
		cfg:      cfg,
		now:      time.Now,
		emails:   make(map[string]*Email),
		sms:      make(map[string]*SMS),
		otps:     make(map[string]*OTP),
		contacts: make(map[string]*Contact),
		credit:   cfg.InitialCredit,
		limiter:  NewRateLimiter(cfg.RateLimitEnabled),
		senders: []Sender{
			{Name: "MySender", Status: "APPROVED"},
			{Name: "MyBrand", Status: "PENDING"},
		},
	}
}

// SetClock replaces the store clock. Intended for tests.
func (s *Store) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// Now returns the current store time.
func (s *Store) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now()
}

// AllowRate applies the rate limiter for key.
func (s *Store) AllowRate(key string, limit int, window time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limiter.Allow(key, limit, window, s.now())
}

// RateLimitEnabled reports whether rate limiting is active.
func (s *Store) RateLimitEnabled() bool { return s.limiter.Enabled() }

// SetRateLimitEnabled toggles rate limiting at runtime.
func (s *Store) SetRateLimitEnabled(enabled bool) { s.limiter.SetEnabled(enabled) }

// Config returns the store configuration.
func (s *Store) Config() Config { return s.cfg }

// ---------------------------------------------------------------------------
// Email
// ---------------------------------------------------------------------------

// AddEmail stores a captured email and returns it.
func (s *Store) AddEmail(e *Email) *Email {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.ID == "" {
		e.ID = NewMessageID()
	}
	if e.Date.IsZero() {
		e.Date = s.now()
	}
	s.emails[e.ID] = e
	s.emailOrder = append(s.emailOrder, e.ID)
	return e
}

// Emails returns captured emails, newest first.
func (s *Store) Emails() []*Email {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Email, 0, len(s.emailOrder))
	for i := len(s.emailOrder) - 1; i >= 0; i-- {
		out = append(out, s.emails[s.emailOrder[i]])
	}
	return out
}

// Email looks up an email by ID.
func (s *Store) Email(id string) (*Email, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.emails[id]
	return e, ok
}

// MarkEmailRead sets the read flag of an email.
func (s *Store) MarkEmailRead(id string, read bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.emails[id]
	if !ok {
		return false
	}
	e.Read = read
	return true
}

// DeleteEmail removes a single email.
func (s *Store) DeleteEmail(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.emails[id]; !ok {
		return false
	}
	delete(s.emails, id)
	s.emailOrder = removeID(s.emailOrder, id)
	return true
}

// ClearEmails removes all captured emails and returns the number removed.
func (s *Store) ClearEmails() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.emailOrder)
	s.emails = make(map[string]*Email)
	s.emailOrder = nil
	return n
}

// EmailCount returns the number of captured emails.
func (s *Store) EmailCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.emailOrder)
}

func removeID(ids []string, id string) []string {
	out := ids[:0]
	for _, v := range ids {
		if v != id {
			out = append(out, v)
		}
	}
	return out
}
