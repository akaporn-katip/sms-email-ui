package web_test

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
	"github.com/akaporn-katip/sms-email-ui/web"
)

func newUI(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()
	cfg := config.Config{Name: "Test Account", Email: "test@example.com", Credit: 50}
	st := store.New(store.Config{
		InitialCredit:  50,
		AccountName:    cfg.Name,
		AccountEmail:   cfg.Email,
		FailureNumbers: []string{"0000000000"},
	})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	api := smsapi.New(st, cfg, logger).Handler()
	return st, web.New(st, cfg, logger, api)
}

func do(t *testing.T, h http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var payload map[string]any
	if rec.Body.Len() > 0 && strings.Contains(rec.Header().Get("Content-Type"), "json") {
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("response is not JSON: %v\n%s", err, rec.Body.String())
		}
	}
	return rec, payload
}

func TestIndexAndAssets(t *testing.T) {
	_, h := newUI(t)

	rec, _ := do(t, h, http.MethodGet, "/", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status = %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("content type = %q", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "smsmail") {
		t.Error("index.html does not look like the UI")
	}

	for _, asset := range []string{"/static/app.js", "/static/style.css"} {
		rec, _ := do(t, h, http.MethodGet, asset, "")
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s status = %d", asset, rec.Code)
		}
	}
}

func TestEmailManagementAPI(t *testing.T) {
	st, h := newUI(t)

	email := st.AddEmail(&store.Email{
		From:    "somchai@example.com",
		To:      []string{"you@example.com"},
		Subject: "ทดสอบ",
		Text:    "hello world",
		HTML:    "<p>hello world</p>",
		Raw:     []byte("Subject: ทดสอบ\r\n\r\nhello world\r\n"),
		Attachments: []store.Attachment{
			{Filename: "doc.txt", ContentType: "text/plain", Size: 5, Data: []byte("hello")},
		},
	})

	rec, body := do(t, h, http.MethodGet, "/api/emails", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	emails, _ := body["emails"].([]any)
	if len(emails) != 1 {
		t.Fatalf("emails = %v", body["emails"])
	}
	summary := emails[0].(map[string]any)
	if summary["subject"] != "ทดสอบ" || summary["preview"] != "hello world" {
		t.Errorf("summary = %v", summary)
	}
	if summary["hasHtml"] != true || summary["attachments"] != float64(1) {
		t.Errorf("summary flags = %v", summary)
	}

	rec, body = do(t, h, http.MethodGet, "/api/emails/"+email.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec.Code)
	}
	if body["html"] != "<p>hello world</p>" {
		t.Errorf("html = %v", body["html"])
	}

	// Search.
	rec, body = do(t, h, http.MethodGet, "/api/emails?q=ทดสอบ", "")
	if rec.Code != http.StatusOK || body["total"] != float64(1) {
		t.Errorf("search result = %v (status %d)", body, rec.Code)
	}
	rec, body = do(t, h, http.MethodGet, "/api/emails?q=nothing-matches", "")
	if rec.Code != http.StatusOK || body["total"] != float64(0) {
		t.Errorf("expected no matches, got %v (status %d)", body, rec.Code)
	}

	// Raw download.
	rec, _ = do(t, h, http.MethodGet, "/api/emails/"+email.ID+"/raw", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "hello world") {
		t.Errorf("raw download failed: status %d", rec.Code)
	}

	// Attachment download.
	rec, _ = do(t, h, http.MethodGet, "/api/emails/"+email.ID+"/attachments/0", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "hello" {
		t.Errorf("attachment = %q (status %d)", rec.Body.String(), rec.Code)
	}
	if rec, _ := do(t, h, http.MethodGet, "/api/emails/"+email.ID+"/attachments/9", ""); rec.Code != http.StatusNotFound {
		t.Errorf("missing attachment status = %d, want 404", rec.Code)
	}

	// Mark read then unread.
	do(t, h, http.MethodPost, "/api/emails/"+email.ID+"/read", "")
	if got, _ := st.Email(email.ID); !got.Read {
		t.Error("expected the email to be marked read")
	}
	do(t, h, http.MethodPost, "/api/emails/"+email.ID+"/read?read=false", "")
	if got, _ := st.Email(email.ID); got.Read {
		t.Error("expected the email to be marked unread")
	}

	// Delete one, then clear.
	rec, _ = do(t, h, http.MethodDelete, "/api/emails/"+email.ID, "")
	if rec.Code != http.StatusOK {
		t.Errorf("delete status = %d", rec.Code)
	}
	if rec, _ := do(t, h, http.MethodDelete, "/api/emails/missing", ""); rec.Code != http.StatusNotFound {
		t.Errorf("delete missing status = %d, want 404", rec.Code)
	}

	st.AddEmail(&store.Email{Subject: "a"})
	st.AddEmail(&store.Email{Subject: "b"})
	rec, body = do(t, h, http.MethodDelete, "/api/emails", "")
	if rec.Code != http.StatusOK || body["deleted"] != float64(2) {
		t.Errorf("clear = %v (status %d)", body, rec.Code)
	}
	if st.EmailCount() != 0 {
		t.Errorf("emails remaining = %d", st.EmailCount())
	}
}

func TestSMSManagementAPI(t *testing.T) {
	st, h := newUI(t)

	if _, err := st.SendSMS("MySender", "0891234567", "hello sms", store.KindSMS); err != nil {
		t.Fatalf("send: %v", err)
	}
	otp, _, err := st.CreateOTP("0891234567", "verify")
	if err != nil {
		t.Fatalf("otp: %v", err)
	}

	rec, body := do(t, h, http.MethodGet, "/api/sms", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	if body["total"] != float64(2) {
		t.Errorf("total = %v, want 2", body["total"])
	}

	rec, body = do(t, h, http.MethodGet, "/api/sms?kind=otp", "")
	if rec.Code != http.StatusOK || body["total"] != float64(1) {
		t.Errorf("otp filter total = %v, want 1 (status %d)", body["total"], rec.Code)
	}
	rec, body = do(t, h, http.MethodGet, "/api/sms?q=hello", "")
	if rec.Code != http.StatusOK || body["total"] != float64(1) {
		t.Errorf("search total = %v, want 1 (status %d)", body["total"], rec.Code)
	}

	// The endpoint integration tests use to read a code the way a human would.
	rec, body = do(t, h, http.MethodGet, "/api/otp/latest?phone=0891234567", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("latest otp status = %d", rec.Code)
	}
	if body["code"] != otp.Code || body["ref"] != otp.Ref {
		t.Errorf("latest otp = %v, want code %s", body, otp.Code)
	}
	if rec, _ := do(t, h, http.MethodGet, "/api/otp/latest?phone=0811111111", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown phone status = %d, want 404", rec.Code)
	}
	if rec, _ := do(t, h, http.MethodGet, "/api/otp/latest", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("missing phone status = %d, want 400", rec.Code)
	}

	rec, body = do(t, h, http.MethodDelete, "/api/sms", "")
	if rec.Code != http.StatusOK || body["deleted"] != float64(2) {
		t.Errorf("clear = %v", body)
	}
}

func TestStatsAndCredit(t *testing.T) {
	_, h := newUI(t)

	rec, body := do(t, h, http.MethodGet, "/api/stats", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("stats status = %d", rec.Code)
	}
	for _, key := range []string{"emails", "messages", "otps", "sms_remaining", "today", "thisMonth"} {
		if _, ok := body[key]; !ok {
			t.Errorf("missing key %q in %v", key, body)
		}
	}

	rec, body = do(t, h, http.MethodPost, "/api/credit", `{"credit":7}`)
	if rec.Code != http.StatusOK || body["sms_remaining"] != float64(7) {
		t.Errorf("set credit = %v (status %d)", body, rec.Code)
	}
	rec, body = do(t, h, http.MethodPost, "/api/credit", `{"add":3}`)
	if rec.Code != http.StatusOK || body["sms_remaining"] != float64(10) {
		t.Errorf("add credit = %v (status %d)", body, rec.Code)
	}
	if rec, _ := do(t, h, http.MethodPost, "/api/credit", `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("empty credit request status = %d, want 400", rec.Code)
	}
}

// TestMockAPIMountedOnWebPort verifies a single base URL works for both the UI
// and the mock provider API.
func TestMockAPIMountedOnWebPort(t *testing.T) {
	_, h := newUI(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/balance", nil)
	req.Header.Set("Authorization", "Bearer sk_live_test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if body["sms_remaining"] != float64(50) {
		t.Errorf("sms_remaining = %v", body["sms_remaining"])
	}
}

// TestEmailListIsSummariesNotBodies pins the contract the web UI depends on.
// The list endpoint returns summaries (no body fields) so that opening a
// message must fetch the detail endpoint; a mismatch between the two shapes
// previously made the UI render an empty body for every message.
func TestEmailListIsSummariesNotBodies(t *testing.T) {
	st, h := newUI(t)

	email := st.AddEmail(&store.Email{
		From:    "a@example.com",
		To:      []string{"b@example.com"},
		Subject: "shape check",
		Text:    "the text body",
		HTML:    "<p>the html body</p>",
		Headers: map[string][]string{"Subject": {"shape check"}},
	})

	_, list := do(t, h, http.MethodGet, "/api/emails", "")
	emails, _ := list["emails"].([]any)
	if len(emails) != 1 {
		t.Fatalf("emails = %v", list["emails"])
	}
	summary := emails[0].(map[string]any)

	for _, absent := range []string{"text", "html", "headers", "raw"} {
		if _, ok := summary[absent]; ok {
			t.Errorf("summary must not carry %q, got %v", absent, summary[absent])
		}
	}
	for _, present := range []string{"id", "subject", "preview", "hasHtml", "attachments"} {
		if _, ok := summary[present]; !ok {
			t.Errorf("summary is missing %q in %v", present, summary)
		}
	}
	if summary["preview"] != "the text body" {
		t.Errorf("preview = %v", summary["preview"])
	}

	_, detail := do(t, h, http.MethodGet, "/api/emails/"+email.ID, "")
	if detail["text"] != "the text body" || detail["html"] != "<p>the html body</p>" {
		t.Errorf("detail is missing the body: %v", detail)
	}
	if _, ok := detail["headers"]; !ok {
		t.Errorf("detail is missing headers: %v", detail)
	}
}

// TestEmailSummaryHasTextFlagForHtmlOnlyMail covers the default tab decision in
// the UI: an HTML-only message has hasHtml true and no text preview.
func TestEmailSummaryHasTextFlagForHtmlOnlyMail(t *testing.T) {
	st, h := newUI(t)

	st.AddEmail(&store.Email{
		From:    "a@example.com",
		To:      []string{"b@example.com"},
		Subject: "html only",
		HTML:    "<p>only html here</p>",
	})

	_, body := do(t, h, http.MethodGet, "/api/emails", "")
	emails, _ := body["emails"].([]any)
	summary := emails[0].(map[string]any)

	if summary["hasHtml"] != true {
		t.Errorf("hasHtml = %v, want true", summary["hasHtml"])
	}
	// The preview falls back to the HTML text so the list is still readable.
	if summary["preview"] != "only html here" {
		t.Errorf("preview = %v, want the stripped HTML", summary["preview"])
	}
}
