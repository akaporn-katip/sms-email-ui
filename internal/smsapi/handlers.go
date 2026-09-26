package smsapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/akaporn-katip/sms-email-ui/internal/store"
)

// ---------------------------------------------------------------------------
// POST /api/v1/sms/send
// ---------------------------------------------------------------------------

type smsSendRequest struct {
	Sender  string `json:"sender"`
	To      string `json:"to"`
	Message string `json:"message"`
}

func (s *Server) handleSMSSend(w http.ResponseWriter, r *http.Request) {
	var req smsSendRequest
	if !s.decode(w, r, &req) {
		return
	}
	if !s.limit("sms:send", limitSMSSend, time.Minute) {
		s.tooManyRequests(w)
		return
	}
	if strings.TrimSpace(req.Sender) == "" || strings.TrimSpace(req.Message) == "" || !store.ValidPhone(req.To) {
		s.badRequest(w, "ข้อมูลไม่ถูกต้อง (sender/to/message)")
		return
	}

	msg, err := s.store.SendSMS(req.Sender, req.To, req.Message, store.KindSMS)
	switch {
	case errors.Is(err, store.ErrInsufficientSMS):
		s.insufficientCredit(w)
		return
	case err != nil:
		s.badRequest(w, "ข้อมูลไม่ถูกต้อง (sender/to/message)")
		return
	}

	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"id":            msg.ID,
		"status":        msg.Status,
		"sms_used":      msg.CreditCost,
		"sms_remaining": s.store.Credit(),
	})
}

// ---------------------------------------------------------------------------
// POST /api/v1/sms/batch
// ---------------------------------------------------------------------------

type smsBatchRequest struct {
	Sender  string   `json:"sender"`
	To      []string `json:"to"`
	Message string   `json:"message"`
}

const maxBatchRecipients = 10000

func (s *Server) handleSMSBatch(w http.ResponseWriter, r *http.Request) {
	var req smsBatchRequest
	if !s.decode(w, r, &req) {
		return
	}
	if !s.limit("sms:batch", limitSMSBatch, time.Minute) {
		s.tooManyRequests(w)
		return
	}
	if strings.TrimSpace(req.Sender) == "" || strings.TrimSpace(req.Message) == "" || len(req.To) == 0 {
		s.badRequest(w, "ข้อมูลไม่ถูกต้อง (sender/to/message)")
		return
	}
	if len(req.To) > maxBatchRecipients {
		s.badRequest(w, "ข้อมูลไม่ถูกต้อง: จำนวนเบอร์เกิน 10,000")
		return
	}

	msgs, err := s.store.SendBatch(req.Sender, req.To, req.Message, store.KindBatch)
	switch {
	case errors.Is(err, store.ErrInsufficientSMS):
		s.insufficientCredit(w)
		return
	case err != nil:
		s.badRequest(w, "ข้อมูลไม่ถูกต้อง (sender/to/message)")
		return
	}

	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"total_messages": len(msgs),
		"sms_used":       len(msgs),
		"sms_remaining":  s.store.Credit(),
	})
}

// ---------------------------------------------------------------------------
// GET /api/v1/sms/status?id=...
// ---------------------------------------------------------------------------

func (s *Server) handleSMSStatus(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		s.badRequest(w, "ข้อมูลไม่ถูกต้อง: ต้องระบุ id")
		return
	}
	msg, ok := s.store.SMSByID(id)
	if !ok {
		writeJSON(w, s.logger, http.StatusNotFound, map[string]any{
			"error": "ไม่พบข้อความนี้",
			"code":  "MESSAGE_NOT_FOUND",
		})
		return
	}
	writeJSON(w, s.logger, http.StatusOK, statusResponse(msg))
}

// ---------------------------------------------------------------------------
// POST /api/v1/sms/scheduled
// ---------------------------------------------------------------------------

type smsScheduledRequest struct {
	To          string `json:"to"`
	Message     string `json:"message"`
	Sender      string `json:"sender"`
	ScheduledAt string `json:"scheduledAt"`
}

func (s *Server) handleSMSScheduled(w http.ResponseWriter, r *http.Request) {
	var req smsScheduledRequest
	if !s.decode(w, r, &req) {
		return
	}
	if !s.limit("sms:scheduled", limitSMSSend, time.Minute) {
		s.tooManyRequests(w)
		return
	}
	if strings.TrimSpace(req.Sender) == "" || strings.TrimSpace(req.Message) == "" || !store.ValidPhone(req.To) {
		s.badRequest(w, "ข้อมูลไม่ถูกต้อง (sender/to/message)")
		return
	}
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ScheduledAt))
	if err != nil {
		s.badRequest(w, "ข้อมูลไม่ถูกต้อง: scheduledAt ต้องเป็นรูปแบบ ISO8601")
		return
	}

	msg, err := s.store.ScheduleSMS(req.Sender, req.To, req.Message, at)
	if err != nil {
		s.badRequest(w, "ข้อมูลไม่ถูกต้อง (sender/to/message)")
		return
	}

	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"id":          msg.ID,
		"status":      msg.Status,
		"scheduledAt": iso(*msg.ScheduledAt),
	})
}

// ---------------------------------------------------------------------------
// POST /api/v1/otp/send
// ---------------------------------------------------------------------------

type otpSendRequest struct {
	Phone   string `json:"phone"`
	Purpose string `json:"purpose"`
}

func (s *Server) handleOTPSend(w http.ResponseWriter, r *http.Request) {
	var req otpSendRequest
	if !s.decode(w, r, &req) {
		return
	}
	if !store.ValidPhone(req.Phone) {
		s.badRequest(w, "เบอร์โทรศัพท์ไม่ถูกต้อง")
		return
	}
	if !s.limit("otp:send:"+store.NormalizePhone(req.Phone), limitOTPSend, 5*time.Minute) {
		s.tooManyRequests(w)
		return
	}

	otp, _, err := s.store.CreateOTP(req.Phone, strings.TrimSpace(req.Purpose))
	switch {
	case errors.Is(err, store.ErrInsufficientSMS):
		s.insufficientCredit(w)
		return
	case errors.Is(err, store.ErrInvalidInput):
		s.badRequest(w, "ข้อมูลไม่ถูกต้อง (phone/purpose)")
		return
	case err != nil:
		s.badRequest(w, "เบอร์โทรศัพท์ไม่ถูกต้อง")
		return
	}

	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"id":           otp.ID,
		"ref":          otp.Ref,
		"phone":        otp.Phone,
		"purpose":      otp.Purpose,
		"expiresAt":    iso(otp.ExpiresAt),
		"expiresIn":    int(store.OTPTTL.Seconds()),
		"smsUsed":      1,
		"smsRemaining": s.store.Credit(),
	})
}

// ---------------------------------------------------------------------------
// POST /api/v1/otp/verify
// ---------------------------------------------------------------------------

type otpVerifyRequest struct {
	Ref  string `json:"ref"`
	Code string `json:"code"`
}

func (s *Server) handleOTPVerify(w http.ResponseWriter, r *http.Request) {
	var req otpVerifyRequest
	if !s.decode(w, r, &req) {
		return
	}
	if !s.limit("otp:verify", limitOTPVerify, 15*time.Minute) {
		s.tooManyRequests(w)
		return
	}
	if strings.TrimSpace(req.Ref) == "" || strings.TrimSpace(req.Code) == "" {
		s.badRequest(w, "ข้อมูลไม่ถูกต้อง: ต้องระบุ ref และ code")
		return
	}

	otp, err := s.store.VerifyOTP(strings.TrimSpace(req.Ref), strings.TrimSpace(req.Code))
	var invalid *store.OTPInvalidError
	switch {
	case err == nil:
		writeJSON(w, s.logger, http.StatusOK, map[string]any{
			"valid":    true,
			"verified": true,
			"ref":      otp.Ref,
			"phone":    otp.Phone,
			"purpose":  otp.Purpose,
		})
	case errors.As(err, &invalid):
		writeJSON(w, s.logger, http.StatusBadRequest, map[string]any{
			"valid":              false,
			"verified":           false,
			"ref":                otp.Ref,
			"phone":              otp.Phone,
			"purpose":            otp.Purpose,
			"error":              "รหัส OTP ไม่ถูกต้อง",
			"code":               "INVALID_OTP",
			"attempts_remaining": invalid.Remaining,
		})
	case errors.Is(err, store.ErrOTPNotFound):
		writeJSON(w, s.logger, http.StatusNotFound, map[string]any{
			"valid":    false,
			"verified": false,
			"error":    "ไม่พบ OTP นี้",
			"code":     "OTP_NOT_FOUND",
		})
	case errors.Is(err, store.ErrOTPExpired):
		writeJSON(w, s.logger, http.StatusGone, map[string]any{
			"valid":    false,
			"verified": false,
			"error":    "OTP หมดอายุ",
			"code":     "OTP_EXPIRED",
		})
	default:
		writeJSON(w, s.logger, http.StatusGone, map[string]any{
			"valid":    false,
			"verified": false,
			"error":    "OTP ถูกล็อคหรือถูกใช้ไปแล้ว",
			"code":     "OTP_LOCKED",
		})
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/contacts
// ---------------------------------------------------------------------------

func (s *Server) handleContactsList(w http.ResponseWriter, r *http.Request) {
	page := intQuery(r, "page", 1)
	perPage := intQuery(r, "per_page", 10)
	if perPage > 500 {
		perPage = 500
	}

	contacts, total := s.store.Contacts(page, perPage)
	totalPages := 0
	if total > 0 {
		totalPages = (total + perPage - 1) / perPage
	}
	if page < 1 {
		page = 1
	}

	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"contacts": contacts,
		"pagination": map[string]any{
			"page":       page,
			"total":      total,
			"totalPages": totalPages,
		},
	})
}

// ---------------------------------------------------------------------------
// POST /api/v1/contacts
// ---------------------------------------------------------------------------

type contactCreateRequest struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
	Email string `json:"email"`
	Tags  string `json:"tags"`
}

func (s *Server) handleContactsCreate(w http.ResponseWriter, r *http.Request) {
	var req contactCreateRequest
	if !s.decode(w, r, &req) {
		return
	}
	contact, err := s.store.AddContact(req.Name, req.Phone, req.Email, req.Tags)
	if err != nil {
		s.badRequest(w, "ข้อมูลไม่ถูกต้อง (name/phone/email)")
		return
	}
	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"id":    contact.ID,
		"name":  contact.Name,
		"phone": contact.Phone,
	})
}

// ---------------------------------------------------------------------------
// POST /api/v1/contacts/import
// ---------------------------------------------------------------------------

type contactsImportRequest struct {
	Contacts []store.Contact `json:"contacts"`
}

func (s *Server) handleContactsImport(w http.ResponseWriter, r *http.Request) {
	var req contactsImportRequest
	if !s.decode(w, r, &req) {
		return
	}
	if !s.limit("contacts:import", limitContactsImport, time.Minute) {
		s.tooManyRequests(w)
		return
	}
	if req.Contacts == nil {
		s.badRequest(w, "ข้อมูลไม่ถูกต้อง: ต้องระบุ contacts")
		return
	}

	imported, skipped, errs := s.store.ImportContacts(req.Contacts)
	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"imported": imported,
		"skipped":  skipped,
		"errors":   errs,
	})
}

// ---------------------------------------------------------------------------
// GET /api/v1/balance
// ---------------------------------------------------------------------------

func (s *Server) handleBalance(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"sms_remaining": s.store.Credit(),
		"name":          s.cfg.Name,
		"email":         s.cfg.Email,
	})
}

// ---------------------------------------------------------------------------
// GET /api/v1/analytics
// ---------------------------------------------------------------------------

func (s *Server) handleAnalytics(w http.ResponseWriter, _ *http.Request) {
	today, thisMonth := s.store.Analytics()
	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"today":     today,
		"thisMonth": thisMonth,
	})
}

// ---------------------------------------------------------------------------
// POST /api/v1/api-keys
// ---------------------------------------------------------------------------

type apiKeyCreateRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req apiKeyCreateRequest
	if !s.decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		s.badRequest(w, "ข้อมูลไม่ถูกต้อง: ต้องระบุ name")
		return
	}
	key := s.store.CreateAPIKey(strings.TrimSpace(req.Name))
	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"id":        key.ID,
		"name":      key.Name,
		"key":       key.Key,
		"createdAt": iso(key.CreatedAt),
	})
}

// ---------------------------------------------------------------------------
// GET /api/v1/senders
// ---------------------------------------------------------------------------

func (s *Server) handleSenders(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.logger, http.StatusOK, map[string]any{
		"senders": s.store.Senders(),
	})
}

func intQuery(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return def
	}
	return n
}
