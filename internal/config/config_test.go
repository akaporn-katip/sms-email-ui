package config_test

import (
	"testing"

	"github.com/akaporn-katip/sms-email-ui/internal/config"
)

func TestDefaults(t *testing.T) {
	cfg, err := config.Parse(nil, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := config.Defaults()
	if cfg.SMTPAddr != want.SMTPAddr || cfg.WebAddr != want.WebAddr || cfg.APIAddr != want.APIAddr {
		t.Errorf("addresses = %q/%q/%q", cfg.SMTPAddr, cfg.WebAddr, cfg.APIAddr)
	}
	if cfg.Credit != 1500 {
		t.Errorf("credit = %d, want 1500", cfg.Credit)
	}
	if len(cfg.FailureNumbers) != 1 || cfg.FailureNumbers[0] != "0000000000" {
		t.Errorf("failure numbers = %v", cfg.FailureNumbers)
	}
}

func TestFlagsOverrideEnvironment(t *testing.T) {
	env := []string{
		"SMSMAIL_SMTP_ADDR=:2025",
		"SMSMAIL_WEB_ADDR=:3025",
		"SMSMAIL_CREDIT=42",
		"SMSMAIL_RATE_LIMIT=true",
		"SMSMAIL_FAILURE_NUMBERS=0000000000,0999999999",
	}

	fromEnv, err := config.Parse(nil, env)
	if err != nil {
		t.Fatalf("Parse(env): %v", err)
	}
	if fromEnv.SMTPAddr != ":2025" || fromEnv.WebAddr != ":3025" {
		t.Errorf("env addresses = %q/%q", fromEnv.SMTPAddr, fromEnv.WebAddr)
	}
	if fromEnv.Credit != 42 || !fromEnv.RateLimit {
		t.Errorf("env credit/ratelimit = %d/%v", fromEnv.Credit, fromEnv.RateLimit)
	}
	if len(fromEnv.FailureNumbers) != 2 {
		t.Errorf("env failure numbers = %v", fromEnv.FailureNumbers)
	}

	fromFlags, err := config.Parse([]string{"-smtp-addr", ":4025", "-credit", "7"}, env)
	if err != nil {
		t.Fatalf("Parse(flags): %v", err)
	}
	if fromFlags.SMTPAddr != ":4025" {
		t.Errorf("flag should win: smtp addr = %q", fromFlags.SMTPAddr)
	}
	if fromFlags.Credit != 7 {
		t.Errorf("flag should win: credit = %d", fromFlags.Credit)
	}
	if fromFlags.WebAddr != ":3025" {
		t.Errorf("env should still apply: web addr = %q", fromFlags.WebAddr)
	}
}

func TestDisableListeners(t *testing.T) {
	cfg, err := config.Parse([]string{"-smtp-addr", "", "-api-addr", "", "-failure-numbers", ""}, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.SMTPAddr != "" || cfg.APIAddr != "" {
		t.Errorf("expected listeners to be disabled, got %q / %q", cfg.SMTPAddr, cfg.APIAddr)
	}
	if len(cfg.FailureNumbers) != 0 {
		t.Errorf("failure numbers = %v, want none", cfg.FailureNumbers)
	}
}

func TestInvalidConfiguration(t *testing.T) {
	if _, err := config.Parse([]string{"-web-addr", "", "-api-addr", ""}, nil); err == nil {
		t.Error("expected an error when every HTTP listener is disabled")
	}
	if _, err := config.Parse([]string{"-credit", "-1"}, nil); err == nil {
		t.Error("expected an error for a negative credit balance")
	}
	if _, err := config.Parse([]string{"-nope"}, nil); err == nil {
		t.Error("expected an error for an unknown flag")
	}
}
