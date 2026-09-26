package store_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/akaporn-katip/sms-email-ui/internal/store"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

func newStore(t *testing.T, credit int) (*store.Store, *clock) {
	t.Helper()
	c := &clock{t: time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)}
	st := store.New(store.Config{
		InitialCredit:  credit,
		AccountName:    "Test Account",
		AccountEmail:   "test@example.com",
		FailureNumbers: []string{"0000000000"},
	})
	st.SetClock(c.now)
	return st, c
}

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"0891234567":   "+66891234567",
		"08-912-34567": "+66891234567",
		"089 123 4567": "+66891234567",
		"+66891234567": "+66891234567",
		"66891234567":  "+66891234567",
		"00891234567":  "+891234567",
		"":             "",
	}
	for in, want := range cases {
		if got := store.NormalizePhone(in); got != want {
			t.Errorf("NormalizePhone(%q) = %q, want %q", in, got, want)
		}
	}
	if !store.ValidPhone("0891234567") {
		t.Error("expected 0891234567 to be valid")
	}
	for _, bad := range []string{"", "abc", "123", "089123456789012345"} {
		if store.ValidPhone(bad) {
			t.Errorf("expected %q to be invalid", bad)
		}
	}
}

func TestSendSMSLifecycle(t *testing.T) {
	st, c := newStore(t, 10)

	msg, err := st.SendSMS("MySender", "0891234567", "hello", store.KindSMS)
	if err != nil {
		t.Fatalf("SendSMS: %v", err)
	}
	if msg.Status != store.StatusPending {
		t.Fatalf("initial status = %q, want pending", msg.Status)
	}
	if msg.Recipient != "+66891234567" {
		t.Errorf("recipient = %q, want E.164", msg.Recipient)
	}
	if msg.RawRecipient != "0891234567" {
		t.Errorf("raw recipient = %q, want the original input", msg.RawRecipient)
	}
	if got := st.Credit(); got != 9 {
		t.Errorf("credit = %d, want 9", got)
	}

	steps := []struct {
		advance time.Duration
		want    string
	}{
		{1500 * time.Millisecond, store.StatusProcessing},
		{1 * time.Second, store.StatusSent},
		{2 * time.Second, store.StatusDelivered},
	}
	for _, step := range steps {
		c.add(step.advance)
		got, ok := st.SMSByID(msg.ID)
		if !ok {
			t.Fatalf("message %s disappeared", msg.ID)
		}
		if got.Status != step.want {
			t.Fatalf("after %s status = %q, want %q", step.advance, got.Status, step.want)
		}
	}

	final, _ := st.SMSByID(msg.ID)
	if final.StatusDetail != store.StatusDetailDelivered {
		t.Errorf("statusDetail = %q, want delivered", final.StatusDetail)
	}
	if final.SentAt == nil || final.DeliveredAt == nil {
		t.Error("expected sentAt and deliveredAt to be set")
	}
	if final.Detail != nil || final.DetailCategory != nil {
		t.Error("expected no failure detail on a delivered message")
	}
}

func TestSendSMSFailureNumber(t *testing.T) {
	st, c := newStore(t, 10)

	msg, err := st.SendSMS("MySender", "0000000000", "will fail", store.KindSMS)
	if err != nil {
		t.Fatalf("SendSMS: %v", err)
	}
	c.add(5 * time.Second)

	got, _ := st.SMSByID(msg.ID)
	if got.Status != store.StatusFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.StatusDetail != store.StatusDetailDeliveryFailed {
		t.Errorf("statusDetail = %q", got.StatusDetail)
	}
	if got.DetailCategory == nil || *got.DetailCategory != store.CategorySubscriberUnreachable {
		t.Errorf("detailCategory = %v, want %s", got.DetailCategory, store.CategorySubscriberUnreachable)
	}
	if got.Detail == nil || *got.Detail != store.DetailSubscriberUnreachable {
		t.Errorf("detail = %v", got.Detail)
	}
}

func TestInsufficientCredit(t *testing.T) {
	st, _ := newStore(t, 1)
	if _, err := st.SendSMS("MySender", "0891234567", "one", store.KindSMS); err != nil {
		t.Fatalf("first send: %v", err)
	}
	_, err := st.SendSMS("MySender", "0891234567", "two", store.KindSMS)
	if !errors.Is(err, store.ErrInsufficientSMS) {
		t.Fatalf("err = %v, want ErrInsufficientSMS", err)
	}
}

func TestSendBatchChargesPerRecipient(t *testing.T) {
	st, _ := newStore(t, 10)
	msgs, err := st.SendBatch("MySender", []string{"0891234567", "0812345678", "0899999999"}, "promo", store.KindBatch)
	if err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("len = %d, want 3", len(msgs))
	}
	if got := st.Credit(); got != 7 {
		t.Errorf("credit = %d, want 7", got)
	}
	if msgs[0].BatchID == "" || msgs[0].BatchID != msgs[1].BatchID {
		t.Error("expected a shared batch id")
	}
}

func TestScheduleSMSStaysPendingUntilDue(t *testing.T) {
	st, c := newStore(t, 10)
	at := c.now().Add(30 * time.Minute)

	msg, err := st.ScheduleSMS("MySender", "0891234567", "later", at)
	if err != nil {
		t.Fatalf("ScheduleSMS: %v", err)
	}
	if msg.Kind != store.KindScheduled {
		t.Errorf("kind = %q", msg.Kind)
	}
	if got := st.Credit(); got != 10 {
		t.Errorf("credit = %d, want 10 (not charged yet)", got)
	}

	c.add(20 * time.Minute)
	got, _ := st.SMSByID(msg.ID)
	if got.Status != store.StatusPending {
		t.Fatalf("status before due = %q, want pending", got.Status)
	}

	c.add(10*time.Minute + 1500*time.Millisecond)
	got, _ = st.SMSByID(msg.ID)
	if got.Status != store.StatusProcessing {
		t.Fatalf("status after due = %q, want processing", got.Status)
	}
}

func TestOTPHappyPath(t *testing.T) {
	st, _ := newStore(t, 10)

	otp, msg, err := st.CreateOTP("0891234567", "verify")
	if err != nil {
		t.Fatalf("CreateOTP: %v", err)
	}
	if otp.Phone != "+66891234567" {
		t.Errorf("phone = %q, want E.164", otp.Phone)
	}
	if len(otp.Code) != store.OTPLength {
		t.Errorf("code length = %d, want %d", len(otp.Code), store.OTPLength)
	}
	if msg.Kind != store.KindOTP || msg.OTPCode != otp.Code {
		t.Error("expected a linked OTP sms record carrying the code")
	}
	if got := st.Credit(); got != 9 {
		t.Errorf("credit = %d, want 9", got)
	}

	verified, err := st.VerifyOTP(otp.Ref, otp.Code)
	if err != nil {
		t.Fatalf("VerifyOTP: %v", err)
	}
	if !verified.Verified {
		t.Error("expected verified = true")
	}

	// A consumed code cannot be reused.
	if _, err := st.VerifyOTP(otp.Ref, otp.Code); !errors.Is(err, store.ErrOTPLocked) {
		t.Errorf("second verify err = %v, want ErrOTPLocked", err)
	}
}

func TestOTPWrongCodeCountsAttempts(t *testing.T) {
	st, _ := newStore(t, 10)
	otp, _, err := st.CreateOTP("0891234567", "login")
	if err != nil {
		t.Fatalf("CreateOTP: %v", err)
	}

	var invalid *store.OTPInvalidError
	for i := 1; i < store.OTPMaxAttempts; i++ {
		_, err := st.VerifyOTP(otp.Ref, "000000")
		if !errors.As(err, &invalid) {
			t.Fatalf("attempt %d: err = %v, want *OTPInvalidError", i, err)
		}
		want := store.OTPMaxAttempts - i
		if invalid.Remaining != want {
			t.Fatalf("attempt %d: remaining = %d, want %d", i, invalid.Remaining, want)
		}
	}

	// The final wrong attempt locks the code.
	if _, err := st.VerifyOTP(otp.Ref, "000000"); !errors.Is(err, store.ErrOTPLocked) {
		t.Fatalf("locking attempt err = %v, want ErrOTPLocked", err)
	}
	// Even the correct code is rejected once locked.
	if _, err := st.VerifyOTP(otp.Ref, otp.Code); !errors.Is(err, store.ErrOTPLocked) {
		t.Fatalf("after lock err = %v, want ErrOTPLocked", err)
	}
}

func TestOTPExpiry(t *testing.T) {
	st, c := newStore(t, 10)
	otp, _, err := st.CreateOTP("0891234567", "verify")
	if err != nil {
		t.Fatalf("CreateOTP: %v", err)
	}
	c.add(store.OTPTTL + time.Second)

	if _, err := st.VerifyOTP(otp.Ref, otp.Code); !errors.Is(err, store.ErrOTPExpired) {
		t.Fatalf("err = %v, want ErrOTPExpired", err)
	}
}

func TestOTPUnknownRefAndPurpose(t *testing.T) {
	st, _ := newStore(t, 10)
	if _, err := st.VerifyOTP("NOPE1234", "123456"); !errors.Is(err, store.ErrOTPNotFound) {
		t.Fatalf("err = %v, want ErrOTPNotFound", err)
	}
	if _, _, err := st.CreateOTP("0891234567", "bogus"); !errors.Is(err, store.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
	if _, _, err := st.CreateOTP("not-a-phone", "verify"); !errors.Is(err, store.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestLatestOTPFor(t *testing.T) {
	st, _ := newStore(t, 10)
	first, _, _ := st.CreateOTP("0891234567", "verify")
	second, _, _ := st.CreateOTP("0891234567", "login")

	got, ok := st.LatestOTPFor("0891234567")
	if !ok {
		t.Fatal("expected an OTP for the phone number")
	}
	if got.Ref != second.Ref || got.Ref == first.Ref {
		t.Fatalf("latest ref = %q, want %q", got.Ref, second.Ref)
	}
	if _, ok := st.LatestOTPFor("0811111111"); ok {
		t.Error("expected no OTP for an unknown number")
	}
}

func TestContactsPaginationAndImport(t *testing.T) {
	st, _ := newStore(t, 10)

	for i := 0; i < 25; i++ {
		if _, err := st.AddContact("Contact", "08912345"+fmt.Sprintf("%02d", i), "", "vip"); err != nil {
			t.Fatalf("AddContact: %v", err)
		}
	}
	page, total := st.Contacts(1, 10)
	if total != 25 {
		t.Fatalf("total = %d, want 25", total)
	}
	if len(page) != 10 {
		t.Fatalf("page size = %d, want 10", len(page))
	}
	last, _ := st.Contacts(3, 10)
	if len(last) != 5 {
		t.Fatalf("last page size = %d, want 5", len(last))
	}

	imported, skipped, errs := st.ImportContacts([]store.Contact{
		{Name: "สมชาย", Phone: "0812345678"},
		{Name: "", Phone: "0812345679"},
		{Name: "NoPhone", Phone: "abc"},
		{Name: "Dup", Phone: "0891234500"},
	})
	if imported != 1 || skipped != 3 {
		t.Fatalf("imported = %d skipped = %d, want 1 and 3", imported, skipped)
	}
	if len(errs) != 3 {
		t.Fatalf("len(errs) = %d, want 3", len(errs))
	}
}

func TestAnalyticsAndSenders(t *testing.T) {
	st, c := newStore(t, 100)

	if _, err := st.SendSMS("MySender", "0891234567", "ok", store.KindSMS); err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := st.SendSMS("NewBrand", "0000000000", "bad", store.KindSMS); err != nil {
		t.Fatalf("send: %v", err)
	}
	c.add(5 * time.Second)

	today, month := st.Analytics()
	if today.Total != 2 || today.Delivered != 1 || today.Failed != 1 {
		t.Fatalf("today = %+v, want total 2 delivered 1 failed 1", today)
	}
	if month.Total != 2 {
		t.Fatalf("month = %+v, want total 2", month)
	}

	found := false
	for _, s := range st.Senders() {
		if s.Name == "NewBrand" && s.Status == "APPROVED" {
			found = true
		}
	}
	if !found {
		t.Error("expected the new sender to be auto-registered as APPROVED")
	}
}

func TestEmailStore(t *testing.T) {
	st, _ := newStore(t, 10)

	e := st.AddEmail(&store.Email{From: "a@example.com", To: []string{"b@example.com"}, Subject: "hi"})
	if e.ID == "" {
		t.Fatal("expected an id")
	}
	if st.EmailCount() != 1 {
		t.Fatalf("count = %d, want 1", st.EmailCount())
	}
	if !st.MarkEmailRead(e.ID, true) {
		t.Fatal("MarkEmailRead returned false")
	}
	if got, _ := st.Email(e.ID); !got.Read {
		t.Error("expected read = true")
	}
	if !st.DeleteEmail(e.ID) {
		t.Fatal("DeleteEmail returned false")
	}
	if st.EmailCount() != 0 {
		t.Fatalf("count = %d, want 0", st.EmailCount())
	}
}

func TestRateLimiter(t *testing.T) {
	now := time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC)

	rl := store.NewRateLimiter(true)
	for i := 0; i < 3; i++ {
		if !rl.Allow("k", 3, time.Minute, now) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if rl.Allow("k", 3, time.Minute, now) {
		t.Fatal("fourth request should be blocked")
	}
	if !rl.Allow("k", 3, time.Minute, now.Add(2*time.Minute)) {
		t.Fatal("request after the window should be allowed")
	}

	off := store.NewRateLimiter(false)
	for i := 0; i < 100; i++ {
		if !off.Allow("k", 1, time.Minute, now) {
			t.Fatal("disabled limiter must allow everything")
		}
	}
}
