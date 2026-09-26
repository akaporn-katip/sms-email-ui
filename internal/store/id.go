package store

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

const idAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// randomAlnum returns a random string of length n over a lowercase
// alphanumeric alphabet.
func randomAlnum(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic("store: cannot read random bytes: " + err.Error())
	}
	var sb strings.Builder
	sb.Grow(n)
	for _, b := range buf {
		sb.WriteByte(idAlphabet[int(b)%len(idAlphabet)])
	}
	return sb.String()
}

// NewMessageID returns a cuid-like identifier such as "cm9abc123xyz".
func NewMessageID() string { return "c" + randomAlnum(23) }

// NewRef returns an 8 character uppercase reference code, e.g. "ABC123EF".
// Ambiguous characters (I, O, 0, 1) are excluded so the ref can be read aloud.
func NewRef() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		panic("store: cannot read random bytes: " + err.Error())
	}
	var sb strings.Builder
	for _, b := range buf {
		sb.WriteByte(alphabet[int(b)%len(alphabet)])
	}
	return sb.String()
}

// NewDigits returns a random numeric string of length n.
func NewDigits(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic("store: cannot read random bytes: " + err.Error())
	}
	var sb strings.Builder
	sb.Grow(n)
	for _, b := range buf {
		sb.WriteByte(byte('0' + int(b)%10))
	}
	return sb.String()
}

func newID(prefix string) string { return prefix + randomAlnum(12) }

// NewSecret returns an API key in the shape the provider uses.
func NewSecret() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		panic("store: cannot read random bytes: " + err.Error())
	}
	return "sk_live_" + hex.EncodeToString(buf)
}
