package store

import (
	"regexp"
	"strings"
)

var (
	e164Re  = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)
	emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// NormalizePhone converts the phone formats the provider accepts into E.164.
// Thai local numbers ("0891234567") become "+66891234567".
func NormalizePhone(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", ".", "").Replace(s)
	switch {
	case strings.HasPrefix(s, "+"):
		return s
	case strings.HasPrefix(s, "00") && len(s) > 2 && s[2] != '0':
		// A real international dialling prefix, e.g. 0066... for Thailand.
		// "00" followed by another zero is not a prefix, which keeps
		// all-zero magic failure numbers usable.
		return "+" + s[2:]
	case strings.HasPrefix(s, "0"):
		return "+66" + s[1:]
	case strings.HasPrefix(s, "66") && len(s) >= 11:
		return "+" + s
	default:
		return "+" + s
	}
}

// ValidPhone reports whether raw is a plausible phone number.
func ValidPhone(raw string) bool { return e164Re.MatchString(NormalizePhone(raw)) }

// ValidEmail reports whether s looks like an email address.
func ValidEmail(s string) bool { return emailRe.MatchString(strings.TrimSpace(s)) }
