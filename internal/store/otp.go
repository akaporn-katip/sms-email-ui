package store

import (
	"errors"
	"fmt"
	"time"
)

// OTP behaviour defaults.
const (
	// OTPLength is the number of digits in a generated code.
	OTPLength = 6
	// OTPTTL is how long a code stays valid.
	OTPTTL = 5 * time.Minute
	// OTPMaxAttempts is the number of wrong codes accepted before locking.
	OTPMaxAttempts = 5
)

// Allowed OTP purposes.
var otpPurposes = map[string]bool{
	"verify":      true,
	"login":       true,
	"transaction": true,
}

// Errors returned by the OTP store.
var (
	ErrOTPNotFound = errors.New("otp not found")
	ErrOTPExpired  = errors.New("otp expired")
	ErrOTPLocked   = errors.New("otp locked")
	ErrOTPInvalid  = errors.New("otp invalid")
)

// OTPInvalidError reports a wrong code together with the remaining attempts.
type OTPInvalidError struct {
	Remaining int
}

func (e *OTPInvalidError) Error() string {
	return fmt.Sprintf("otp code is invalid (%d attempts remaining)", e.Remaining)
}

func (e *OTPInvalidError) Unwrap() error { return ErrOTPInvalid }

// CreateOTP generates a code for phone, records the outbound SMS that would
// carry it, and consumes one credit.
func (s *Store) CreateOTP(phoneRaw, purpose string) (*OTP, *SMS, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advanceLocked()

	if !ValidPhone(phoneRaw) {
		return nil, nil, ErrInvalidInput
	}
	if purpose == "" {
		purpose = "verify"
	}
	if !otpPurposes[purpose] {
		return nil, nil, ErrInvalidInput
	}
	if s.credit < 1 {
		return nil, nil, ErrInsufficientSMS
	}
	s.credit--

	now := s.now()
	otp := &OTP{
		ID:          NewMessageID(),
		Ref:         NewRef(),
		Phone:       NormalizePhone(phoneRaw),
		PhoneRaw:    phoneRaw,
		Purpose:     purpose,
		Code:        NewDigits(OTPLength),
		MaxAttempts: OTPMaxAttempts,
		CreatedAt:   now,
		ExpiresAt:   now.Add(OTPTTL),
	}

	msg := &SMS{
		ID:           NewMessageID(),
		Kind:         KindOTP,
		Sender:       "MySender",
		Recipient:    otp.Phone,
		RawRecipient: phoneRaw,
		Message:      fmt.Sprintf("รหัส OTP ของคุณคือ %s (หมดอายุใน 5 นาที)", otp.Code),
		Status:       StatusPending,
		StatusDetail: StatusDetailUnknown,
		CreditCost:   1,
		CreatedAt:    now,
		Ref:          otp.Ref,
		OTPCode:      otp.Code,
	}
	msg.advance(now, s.isFailureLocked(msg.Recipient))

	s.otps[otp.Ref] = otp
	s.otpOrder = append(s.otpOrder, otp.Ref)
	s.sms[msg.ID] = msg
	s.smsOrder = append(s.smsOrder, msg.ID)

	return otp, msg, nil
}

// VerifyOTP checks a code against a reference.
//
// It returns ErrOTPNotFound when the reference is unknown, ErrOTPExpired or
// ErrOTPLocked (both mapped to HTTP 410 by the API) when the code can no longer
// be used, and an *OTPInvalidError when the code is wrong but attempts remain.
func (s *Store) VerifyOTP(ref, code string) (*OTP, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	otp, ok := s.otps[ref]
	if !ok {
		return nil, ErrOTPNotFound
	}
	switch {
	case otp.Locked:
		return nil, ErrOTPLocked
	case otp.ConsumedAt != nil:
		return nil, ErrOTPLocked
	case !s.now().Before(otp.ExpiresAt):
		return nil, ErrOTPExpired
	}

	if code != otp.Code {
		otp.Attempts++
		if otp.Attempts >= otp.MaxAttempts {
			otp.Locked = true
			return nil, ErrOTPLocked
		}
		return otp, &OTPInvalidError{Remaining: otp.MaxAttempts - otp.Attempts}
	}

	now := s.now()
	otp.Verified = true
	otp.ConsumedAt = &now
	return otp, nil
}

// OTPs returns generated codes, newest first.
func (s *Store) OTPs() []*OTP {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*OTP, 0, len(s.otpOrder))
	for i := len(s.otpOrder) - 1; i >= 0; i-- {
		out = append(out, s.otps[s.otpOrder[i]])
	}
	return out
}

// OTPByRef looks up a code by reference.
func (s *Store) OTPByRef(ref string) (*OTP, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.otps[ref]
	return o, ok
}

// LatestOTPFor returns the most recent code issued for phone, valid or not.
func (s *Store) LatestOTPFor(phone string) (*OTP, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := NormalizePhone(phone)
	for i := len(s.otpOrder) - 1; i >= 0; i-- {
		o := s.otps[s.otpOrder[i]]
		if o.Phone == want {
			return o, true
		}
	}
	return nil, false
}

// DeleteOTP removes a code by reference.
func (s *Store) DeleteOTP(ref string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.otps[ref]; !ok {
		return false
	}
	delete(s.otps, ref)
	s.otpOrder = removeID(s.otpOrder, ref)
	return true
}

// ClearOTPs removes all codes and returns the number removed.
func (s *Store) ClearOTPs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.otpOrder)
	s.otps = make(map[string]*OTP)
	s.otpOrder = nil
	return n
}

// OTPCount returns the number of stored codes.
func (s *Store) OTPCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.otpOrder)
}
