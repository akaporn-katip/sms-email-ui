package smtpd_test

import (
	"io"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/akaporn-katip/sms-email-ui/internal/smtpd"
	"github.com/akaporn-katip/sms-email-ui/internal/store"
)

func encodedSubject(t *testing.T, subject string) string {
	t.Helper()
	return mime.BEncoding.Encode("UTF-8", subject)
}

// rawMessage builds a multipart message with a plain-text part, an HTML part
// and one attachment.
func rawMessage(t *testing.T, subject string) string {
	t.Helper()
	var sb strings.Builder
	write := func(lines ...string) {
		for _, l := range lines {
			sb.WriteString(l)
			sb.WriteString("\r\n")
		}
	}
	write(
		"From: Somchai Jaidee <somchai@example.com>",
		"To: You <you@example.com>",
		"Cc: cc@example.com",
		"Subject: "+encodedSubject(t, subject),
		"Date: Mon, 09 Mar 2026 10:00:00 +0700",
		"Message-ID: <abc123@example.com>",
		"MIME-Version: 1.0",
		`Content-Type: multipart/mixed; boundary="BOUND"`,
		"",
		"--BOUND",
		`Content-Type: multipart/alternative; boundary="ALT"`,
		"",
		"--ALT",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		"สวัสดีครับ ทดสอบ",
		"--ALT",
		"Content-Type: text/html; charset=UTF-8",
		"",
		"<p>สวัสดีครับ <b>ทดสอบ</b></p>",
		"--ALT--",
		"--BOUND",
		`Content-Type: application/pdf; name="doc.pdf"`,
		`Content-Disposition: attachment; filename="doc.pdf"`,
		"Content-Transfer-Encoding: base64",
		"",
		"aGVsbG8=",
		"--BOUND--",
	)
	return sb.String()
}

func TestParseMultipartMessage(t *testing.T) {
	const subject = "ทดสอบระบบอีเมล"
	email := smtpd.Parse([]byte(rawMessage(t, subject)))

	if email.ID == "" {
		t.Error("expected an id")
	}
	if email.Subject != subject {
		t.Errorf("subject = %q, want %q", email.Subject, subject)
	}
	if email.From != "somchai@example.com" {
		t.Errorf("from = %q", email.From)
	}
	if len(email.To) != 1 || email.To[0] != "you@example.com" {
		t.Errorf("to = %v", email.To)
	}
	if len(email.Cc) != 1 || email.Cc[0] != "cc@example.com" {
		t.Errorf("cc = %v", email.Cc)
	}
	if !strings.Contains(email.Text, "สวัสดีครับ ทดสอบ") {
		t.Errorf("text = %q", email.Text)
	}
	if !strings.Contains(email.HTML, "<b>ทดสอบ</b>") {
		t.Errorf("html = %q", email.HTML)
	}
	if len(email.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(email.Attachments))
	}
	att := email.Attachments[0]
	if att.Filename != "doc.pdf" || att.ContentType != "application/pdf" || string(att.Data) != "hello" {
		t.Errorf("attachment = %+v (data %q)", att, string(att.Data))
	}
	if email.Size != int64(len(rawMessage(t, subject))) {
		t.Errorf("size = %d", email.Size)
	}
	if len(email.Headers["Message-Id"]) == 0 && len(email.Headers["Message-ID"]) == 0 {
		t.Errorf("expected raw headers to be kept, got %v", email.Headers)
	}
	if email.Date.IsZero() {
		t.Error("expected a parsed date")
	}
}

func TestParseLatin1Body(t *testing.T) {
	raw := "From: a@example.com\r\n" +
		"To: b@example.com\r\n" +
		"Subject: plain\r\n" +
		"Content-Type: text/plain; charset=iso-8859-1\r\n" +
		"Content-Transfer-Encoding: 8bit\r\n" +
		"\r\n" +
		"caf\xe9\r\n"

	email := smtpd.Parse([]byte(raw))
	if !strings.Contains(email.Text, "café") {
		t.Errorf("text = %q, want the ISO-8859-1 body decoded to UTF-8", email.Text)
	}
}

func TestParseMalformedMessageFallsBackToRaw(t *testing.T) {
	email := smtpd.Parse([]byte("this is not a message at all"))
	if email.ID == "" {
		t.Error("expected an id even for a malformed message")
	}
	if !strings.Contains(email.Text, "this is not a message") {
		t.Errorf("text = %q, want the raw body", email.Text)
	}
}

func TestSMTPRoundTrip(t *testing.T) {
	st := store.New(store.Config{InitialCredit: 10})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := smtpd.NewServer(ln.Addr().String(), st, logger)
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	addr := ln.Addr().String()
	body := rawMessage(t, "ผ่าน SMTP")

	// Give the listener a moment to be ready.
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never came up: %v", err)
		}
	}

	if err := smtp.SendMail(addr, nil, "sender@example.com", []string{"rcpt@example.com"}, []byte(body)); err != nil {
		t.Fatalf("SendMail: %v", err)
	}

	emails := st.Emails()
	if len(emails) != 1 {
		t.Fatalf("captured %d emails, want 1", len(emails))
	}
	got := emails[0]
	if got.Subject != "ผ่าน SMTP" {
		t.Errorf("subject = %q", got.Subject)
	}
	if got.From != "somchai@example.com" {
		t.Errorf("from = %q, want the header value", got.From)
	}
	if len(got.Attachments) != 1 {
		t.Errorf("attachments = %d, want 1", len(got.Attachments))
	}
}

func TestSMTPAcceptsAnyCredentials(t *testing.T) {
	st := store.New(store.Config{InitialCredit: 10})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := smtpd.NewServer(ln.Addr().String(), st, logger)
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	addr := ln.Addr().String()
	deadline := time.Now().Add(2 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never came up: %v", err)
		}
	}

	client, err := smtp.Dial(addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	if ok, _ := client.Extension("AUTH"); !ok {
		t.Fatal("expected the server to advertise AUTH")
	}
	if err := client.Auth(smtp.PlainAuth("", "any-user", "any-password", "127.0.0.1")); err != nil {
		t.Fatalf("Auth: %v", err)
	}

	if err := client.Mail("someone@example.com"); err != nil {
		t.Fatalf("MAIL FROM: %v", err)
	}
	if err := client.Rcpt("rcpt@example.com"); err != nil {
		t.Fatalf("RCPT TO: %v", err)
	}
	w, err := client.Data()
	if err != nil {
		t.Fatalf("DATA: %v", err)
	}
	if _, err := w.Write([]byte("Subject: auth test\r\n\r\nhello\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := client.Quit(); err != nil {
		t.Fatalf("QUIT: %v", err)
	}

	if st.EmailCount() != 1 {
		t.Fatalf("captured %d emails, want 1", st.EmailCount())
	}
}
