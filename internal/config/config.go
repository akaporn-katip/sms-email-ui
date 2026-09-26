// Package config holds the runtime configuration for the smsmail server.
package config

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
)

// Config is the fully resolved runtime configuration.
type Config struct {
	// SMTPAddr is the address the fake SMTP server listens on.
	SMTPAddr string
	// WebAddr serves the web UI and the management API.
	WebAddr string
	// APIAddr serves the mock SMS API. It is also mounted on WebAddr
	// under /api/v1/ so that a single base URL works for both.
	APIAddr string

	// APIKey, when set, is the only bearer token accepted by the mock API.
	// When empty any "sk_..." token is accepted.
	APIKey string
	// RateLimit enables the default rate limits.
	RateLimit bool
	// FailureNumbers are recipient numbers that always fail delivery.
	FailureNumbers []string

	// Credit is the starting SMS balance.
	Credit int
	// Name and Email are reported by GET /api/v1/balance.
	Name  string
	Email string
}

// Defaults returns the configuration used when no flags or environment
// variables are given.
func Defaults() Config {
	return Config{
		SMTPAddr:       ":1025",
		WebAddr:        ":8025",
		APIAddr:        ":8080",
		RateLimit:      false,
		FailureNumbers: []string{"0000000000"},
		Credit:         1500,
		Name:           "Local Developer",
		Email:          "dev@example.com",
	}
}

// Parse resolves the configuration from environment variables and flags.
// Flags take precedence over environment variables, which take precedence over
// defaults.
func Parse(args []string, environ []string) (Config, error) {
	cfg := Defaults()
	env := envMap(environ)

	applyEnv(&cfg, env)

	fs := flag.NewFlagSet("smsmail", flag.ContinueOnError)
	fs.StringVar(&cfg.SMTPAddr, "smtp-addr", cfg.SMTPAddr, "SMTP listen address (set empty to disable)")
	fs.StringVar(&cfg.WebAddr, "web-addr", cfg.WebAddr, "web UI and management API listen address")
	fs.StringVar(&cfg.APIAddr, "api-addr", cfg.APIAddr, "mock SMS API listen address (set empty to disable)")
	fs.StringVar(&cfg.APIKey, "api-key", cfg.APIKey, "require this exact bearer token (default: accept any sk_... token)")
	fs.BoolVar(&cfg.RateLimit, "rate-limit", cfg.RateLimit, "enforce rate limits")
	fs.StringVar(&cfg.Name, "account-name", cfg.Name, "account name reported by /api/v1/balance")
	fs.StringVar(&cfg.Email, "account-email", cfg.Email, "account email reported by /api/v1/balance")
	fs.IntVar(&cfg.Credit, "credit", cfg.Credit, "starting SMS credit balance")
	failures := fs.String("failure-numbers", strings.Join(cfg.FailureNumbers, ","), "comma separated recipient numbers that always fail delivery")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if strings.TrimSpace(*failures) == "" {
		cfg.FailureNumbers = nil
	} else {
		parts := strings.Split(*failures, ",")
		cfg.FailureNumbers = make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				cfg.FailureNumbers = append(cfg.FailureNumbers, p)
			}
		}
	}

	if cfg.Credit < 0 {
		return Config{}, fmt.Errorf("credit must not be negative")
	}
	if cfg.WebAddr == "" && cfg.APIAddr == "" {
		return Config{}, fmt.Errorf("web-addr and api-addr cannot both be empty")
	}
	return cfg, nil
}

func envMap(environ []string) map[string]string {
	out := make(map[string]string, len(environ))
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			out[k] = v
		}
	}
	return out
}

func applyEnv(cfg *Config, env map[string]string) {
	if v, ok := env["SMSMAIL_SMTP_ADDR"]; ok {
		cfg.SMTPAddr = v
	}
	if v, ok := env["SMSMAIL_WEB_ADDR"]; ok {
		cfg.WebAddr = v
	}
	if v, ok := env["SMSMAIL_API_ADDR"]; ok {
		cfg.APIAddr = v
	}
	if v, ok := env["SMSMAIL_API_KEY"]; ok {
		cfg.APIKey = v
	}
	if v, ok := env["SMSMAIL_ACCOUNT_NAME"]; ok {
		cfg.Name = v
	}
	if v, ok := env["SMSMAIL_ACCOUNT_EMAIL"]; ok {
		cfg.Email = v
	}
	if v, ok := env["SMSMAIL_RATE_LIMIT"]; ok {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.RateLimit = b
		}
	}
	if v, ok := env["SMSMAIL_CREDIT"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Credit = n
		}
	}
	if v, ok := env["SMSMAIL_FAILURE_NUMBERS"]; ok {
		cfg.FailureNumbers = nil
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				cfg.FailureNumbers = append(cfg.FailureNumbers, p)
			}
		}
	}
}
