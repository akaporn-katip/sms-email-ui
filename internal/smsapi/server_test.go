package smsapi_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akaporn-katip/sms-email-ui/internal/config"
	"github.com/akaporn-katip/sms-email-ui/internal/smsapi"
	"github.com/akaporn-katip/sms-email-ui/internal/store"
)

const token = "sk_live_test_token"

func newServer(t *testing.T, cfg config.Config) (*store.Store, http.Handler) {
	t.Helper()
	cfg.Name = "Test Account"
	cfg.Email = "test@example.com"
	st := store.New(store.Config{
		InitialCredit:    100,
		AccountName:      cfg.Name,
		AccountEmail:     cfg.Email,
		RateLimitEnabled: cfg.RateLimit,
		FailureNumbers:   []string{"0000000000"},
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return st, smsapi.New(st, cfg, logger).Handler()
}

func request(t *testing.T, h http.Handler, method, path, body string, auth string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var payload map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("response is not a JSON object: %v\n%s", err, rec.Body.String())
		}
	}
	return rec, payload
}

func TestAuthentication(t *testing.T) {
	_, h := newServer(t, config.Config{})

	if rec, _ := request(t, h, http.MethodGet, "/api/v1/balance", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("missing token: status = %d, want 401", rec.Code)
	}
	if rec, _ := request(t, h, http.MethodGet, "/api/v1/balance", "", "nope"); rec.Code != http.StatusUnauthorized {
		t.Errorf("bad token: status = %d, want 401", rec.Code)
	}
	if rec, _ := request(t, h, http.MethodGet, "/api/v1/balance", "", token); rec.Code != http.StatusOK {
		t.Errorf("good token: status = %d, want 200", rec.Code)
	}
}

func TestExactAPIKeyWhenConfigured(t *testing.T) {
	_, h := newServer(t, config.Config{APIKey: "sk_fixed"})

	if rec, _ := request(t, h, http.MethodGet, "/api/v1/balance", "", "sk_other"); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong key: status = %d, want 401", rec.Code)
	}
	if rec, _ := request(t, h, http.MethodGet, "/api/v1/balance", "", "sk_fixed"); rec.Code != http.StatusOK {
		t.Errorf("right key: status = %d, want 200", rec.Code)
	}
}

func TestSendSMSSResponseShape(t *testing.T) {
	st, h := newServer(t, config.Config{})

	rec, body := request(t, h, http.MethodPost, "/api/v1/sms/send",
		`{"sender":"MySender","to":"0891234567","message":"สวัสดีครับ"}`, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	for _, key := range []string{"id", "status", "sms_used", "sms_remaining"} {
		if _, ok := body[key]; !ok {
			t.Errorf("missing key %q in %v", key, body)
		}
	}
	if body["status"] != "pending" {
		t.Errorf("status = %v, want pending", body["status"])
	}
	if body["sms_used"] != float64(1) {
		t.Errorf("sms_used = %v, want 1", body["sms_used"])
	}
	if body["sms_remaining"] != float64(99) {
		t.Errorf("sms_remaining = %v, want 99", body["sms_remaining"])
	}

	id, _ := body["id"].(string)
	status, ok := st.SMSByID(id)
	if !ok {
		t.Fatalf("message %q was not recorded", id)
	}
	if status.Message != "สวัสดีครับ" {
		t.Errorf("stored message = %q", status.Message)
	}
}

func TestSendSMSValidation(t *testing.T) {
	_, h := newServer(t, config.Config{})
	cases := []string{
		`{"sender":"","to":"0891234567","message":"hi"}`,
		`{"sender":"MySender","to":"nope","message":"hi"}`,
		`{"sender":"MySender","to":"0891234567","message":""}`,
		`{not json}`,
	}
	for _, body := range cases {
		rec, _ := request(t, h, http.MethodPost, "/api/v1/sms/send", body, token)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestBatchSendCountsMessages(t *testing.T) {
	_, h := newServer(t, config.Config{})

	rec, body := request(t, h, http.MethodPost, "/api/v1/sms/batch",
		`{"sender":"MySender","to":["0891234567","0812345678","0899999999"],"message":"โปรโมชัน"}`, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if body["total_messages"] != float64(3) {
		t.Errorf("total_messages = %v, want 3", body["total_messages"])
	}
	if body["sms_used"] != float64(3) || body["sms_remaining"] != float64(97) {
		t.Errorf("credits = %v / %v, want 3 / 97", body["sms_used"], body["sms_remaining"])
	}

	rec, _ = request(t, h, http.MethodPost, "/api/v1/sms/batch",
		`{"sender":"MySender","to":[],"message":"x"}`, token)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty recipient list: status = %d, want 400", rec.Code)
	}
}

func TestSMSStatusShape(t *testing.T) {
	st, h := newServer(t, config.Config{})

	_, sent := request(t, h, http.MethodPost, "/api/v1/sms/send",
		`{"sender":"MySender","to":"0891234567","message":"hi"}`, token)
	id := sent["id"].(string)

	rec, body := request(t, h, http.MethodGet, "/api/v1/sms/status?id="+id, "", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	for _, key := range []string{
		"id", "recipient", "status", "statusDetail", "detail", "detailCategory",
		"senderName", "creditCost", "sentAt", "deliveredAt",
	} {
		if _, ok := body[key]; !ok {
			t.Errorf("missing key %q in %v", key, body)
		}
	}
	// The recipient is echoed in the format the caller supplied.
	if body["recipient"] != "0891234567" {
		t.Errorf("recipient = %v, want the original input", body["recipient"])
	}
	if body["senderName"] != "MySender" {
		t.Errorf("senderName = %v", body["senderName"])
	}
	if body["detail"] != nil || body["detailCategory"] != nil {
		t.Errorf("expected null detail fields, got %v / %v", body["detail"], body["detailCategory"])
	}
	if body["deliveredAt"] != nil {
		t.Errorf("deliveredAt = %v, want null while pending", body["deliveredAt"])
	}

	if rec, _ := request(t, h, http.MethodGet, "/api/v1/sms/status?id=missing", "", token); rec.Code != http.StatusNotFound {
		t.Errorf("unknown id: status = %d, want 404", rec.Code)
	}
	if rec, _ := request(t, h, http.MethodGet, "/api/v1/sms/status", "", token); rec.Code != http.StatusBadRequest {
		t.Errorf("missing id: status = %d, want 400", rec.Code)
	}
	_ = st
}

func TestScheduledSMS(t *testing.T) {
	_, h := newServer(t, config.Config{})

	rec, body := request(t, h, http.MethodPost, "/api/v1/sms/scheduled",
		`{"to":"0891234567","message":"นัดหมาย","sender":"MySender","scheduledAt":"2030-03-10T03:00:00Z"}`, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if body["status"] != "pending" {
		t.Errorf("status = %v", body["status"])
	}
	if body["scheduledAt"] != "2030-03-10T03:00:00Z" {
		t.Errorf("scheduledAt = %v", body["scheduledAt"])
	}
	if id, _ := body["id"].(string); !strings.HasPrefix(id, "sch_") {
		t.Errorf("id = %v, want an sch_ prefix", body["id"])
	}

	rec, _ = request(t, h, http.MethodPost, "/api/v1/sms/scheduled",
		`{"to":"0891234567","message":"x","sender":"MySender","scheduledAt":"tomorrow"}`, token)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad date: status = %d, want 400", rec.Code)
	}
}

func TestOTPFlow(t *testing.T) {
	st, h := newServer(t, config.Config{})

	rec, body := request(t, h, http.MethodPost, "/api/v1/otp/send",
		`{"phone":"0891234567","purpose":"verify"}`, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	for _, key := range []string{"id", "ref", "phone", "purpose", "expiresAt", "expiresIn", "smsUsed", "smsRemaining"} {
		if _, ok := body[key]; !ok {
			t.Errorf("missing key %q in %v", key, body)
		}
	}
	if body["phone"] != "+66891234567" {
		t.Errorf("phone = %v, want E.164", body["phone"])
	}
	if body["expiresIn"] != float64(300) {
		t.Errorf("expiresIn = %v, want 300", body["expiresIn"])
	}
	ref := body["ref"].(string)

	otp, ok := st.LatestOTPFor("0891234567")
	if !ok {
		t.Fatal("expected the code to be stored")
	}

	// Wrong code: 400 with the remaining attempt count.
	rec, body = request(t, h, http.MethodPost, "/api/v1/otp/verify",
		`{"ref":"`+ref+`","code":"000000"}`, token)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong code: status = %d, body = %s", rec.Code, rec.Body)
	}
	if body["valid"] != false || body["verified"] != false {
		t.Errorf("expected valid/verified false, got %v/%v", body["valid"], body["verified"])
	}
	if body["attempts_remaining"] != float64(4) {
		t.Errorf("attempts_remaining = %v, want 4", body["attempts_remaining"])
	}

	// Correct code.
	rec, body = request(t, h, http.MethodPost, "/api/v1/otp/verify",
		`{"ref":"`+ref+`","code":"`+otp.Code+`"}`, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("correct code: status = %d, body = %s", rec.Code, rec.Body)
	}
	if body["valid"] != true || body["verified"] != true {
		t.Errorf("expected valid/verified true, got %v/%v", body["valid"], body["verified"])
	}
	if body["ref"] != ref || body["phone"] != "+66891234567" {
		t.Errorf("unexpected payload: %v", body)
	}

	// Reuse is rejected with 410.
	rec, _ = request(t, h, http.MethodPost, "/api/v1/otp/verify",
		`{"ref":"`+ref+`","code":"`+otp.Code+`"}`, token)
	if rec.Code != http.StatusGone {
		t.Errorf("reuse: status = %d, want 410", rec.Code)
	}
}

func TestOTPErrors(t *testing.T) {
	_, h := newServer(t, config.Config{})

	rec, _ := request(t, h, http.MethodPost, "/api/v1/otp/send", `{"phone":"abc"}`, token)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad phone: status = %d, want 400", rec.Code)
	}

	rec, _ = request(t, h, http.MethodPost, "/api/v1/otp/send", `{"phone":"0891234567","purpose":"bogus"}`, token)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad purpose: status = %d, want 400", rec.Code)
	}

	rec, _ = request(t, h, http.MethodPost, "/api/v1/otp/verify", `{"ref":"UNKNOWN1","code":"123456"}`, token)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown ref: status = %d, want 404", rec.Code)
	}
}

func TestInsufficientCreditReturns402(t *testing.T) {
	_, h := newServer(t, config.Config{})
	// Drain the balance.
	for i := 0; i < 100; i++ {
		request(t, h, http.MethodPost, "/api/v1/sms/send", `{"sender":"S","to":"0891234567","message":"x"}`, token)
	}
	rec, body := request(t, h, http.MethodPost, "/api/v1/sms/send",
		`{"sender":"S","to":"0891234567","message":"x"}`, token)
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402 (body %s)", rec.Code, rec.Body)
	}
	if body["code"] != "INSUFFICIENT_SMS" {
		t.Errorf("code = %v", body["code"])
	}
}

func TestRateLimitReturns429(t *testing.T) {
	_, h := newServer(t, config.Config{RateLimit: true})

	blocked := false
	for i := 0; i < 15; i++ {
		rec, _ := request(t, h, http.MethodPost, "/api/v1/sms/send",
			`{"sender":"S","to":"0891234567","message":"x"}`, token)
		if rec.Code == http.StatusTooManyRequests {
			blocked = true
			break
		}
	}
	if !blocked {
		t.Error("expected a 429 once the 10 req/min SMS limit was exceeded")
	}
}

func TestContacts(t *testing.T) {
	_, h := newServer(t, config.Config{})

	rec, body := request(t, h, http.MethodPost, "/api/v1/contacts",
		`{"name":"สมชาย ใจดี","phone":"0891234567","email":"somchai@example.com","tags":"vip,customer"}`, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if body["name"] != "สมชาย ใจดี" || body["phone"] != "+66891234567" {
		t.Errorf("unexpected contact: %v", body)
	}
	if !strings.HasPrefix(body["id"].(string), "ct_") {
		t.Errorf("id = %v, want a ct_ prefix", body["id"])
	}

	rec, body = request(t, h, http.MethodGet, "/api/v1/contacts", "", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	contacts, ok := body["contacts"].([]any)
	if !ok || len(contacts) != 1 {
		t.Fatalf("contacts = %v", body["contacts"])
	}
	pagination, ok := body["pagination"].(map[string]any)
	if !ok {
		t.Fatalf("pagination missing: %v", body)
	}
	if pagination["page"] != float64(1) || pagination["total"] != float64(1) || pagination["totalPages"] != float64(1) {
		t.Errorf("pagination = %v", pagination)
	}

	rec, _ = request(t, h, http.MethodPost, "/api/v1/contacts", `{"name":"","phone":"0891234567"}`, token)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid contact: status = %d, want 400", rec.Code)
	}
}

func TestContactsImport(t *testing.T) {
	_, h := newServer(t, config.Config{})

	rec, body := request(t, h, http.MethodPost, "/api/v1/contacts/import",
		`{"contacts":[{"name":"สมชาย","phone":"0891234567"},{"name":"สมหญิง","phone":"0812345678"},{"name":"bad","phone":"xyz"}]}`, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if body["imported"] != float64(2) || body["skipped"] != float64(1) {
		t.Errorf("imported/skipped = %v/%v, want 2/1", body["imported"], body["skipped"])
	}
	errs, ok := body["errors"].([]any)
	if !ok {
		t.Fatalf("errors = %v, want an array", body["errors"])
	}
	if len(errs) != 1 {
		t.Errorf("len(errors) = %d, want 1", len(errs))
	}
}

func TestBalanceAndAnalytics(t *testing.T) {
	_, h := newServer(t, config.Config{})

	rec, body := request(t, h, http.MethodGet, "/api/v1/balance", "", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if body["sms_remaining"] != float64(100) || body["name"] != "Test Account" || body["email"] != "test@example.com" {
		t.Errorf("balance = %v", body)
	}

	request(t, h, http.MethodPost, "/api/v1/sms/send", `{"sender":"S","to":"0891234567","message":"x"}`, token)

	rec, body = request(t, h, http.MethodGet, "/api/v1/analytics", "", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	today, ok := body["today"].(map[string]any)
	if !ok {
		t.Fatalf("today missing: %v", body)
	}
	if today["total"] != float64(1) {
		t.Errorf("today.total = %v, want 1", today["total"])
	}
	if _, ok := body["thisMonth"].(map[string]any); !ok {
		t.Errorf("thisMonth missing: %v", body)
	}
}

func TestCreateAPIKey(t *testing.T) {
	_, h := newServer(t, config.Config{})

	rec, body := request(t, h, http.MethodPost, "/api/v1/api-keys", `{"name":"Production Key"}`, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	for _, key := range []string{"id", "name", "key", "createdAt"} {
		if _, ok := body[key]; !ok {
			t.Errorf("missing key %q in %v", key, body)
		}
	}
	if !strings.HasPrefix(body["key"].(string), "sk_live_") {
		t.Errorf("key = %v, want an sk_live_ prefix", body["key"])
	}
	if _, ok := body["createdAt"].(string); !ok {
		t.Errorf("createdAt = %v, want an ISO8601 string", body["createdAt"])
	}

	rec, _ = request(t, h, http.MethodPost, "/api/v1/api-keys", `{"name":""}`, token)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing name: status = %d, want 400", rec.Code)
	}
}

func TestSenders(t *testing.T) {
	_, h := newServer(t, config.Config{})

	rec, body := request(t, h, http.MethodGet, "/api/v1/senders", "", token)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	senders, ok := body["senders"].([]any)
	if !ok || len(senders) == 0 {
		t.Fatalf("senders = %v", body["senders"])
	}
	first := senders[0].(map[string]any)
	if first["name"] != "MySender" || first["status"] != "APPROVED" {
		t.Errorf("first sender = %v", first)
	}
}

func TestCORSPreflight(t *testing.T) {
	_, h := newServer(t, config.Config{})
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/sms/send", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("missing CORS header")
	}
}
