// Command smsmail runs a local email and SMS testing server.
//
// It captures outgoing SMTP mail and implements the SMS provider REST API v1
// against an in-memory store, so applications can be tested end to end without
// contacting a real mail server or SMS gateway.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/emersion/go-smtp"

	"github.com/akaporn-katip/sms-email-ui/internal/config"
	"github.com/akaporn-katip/sms-email-ui/internal/smsapi"
	"github.com/akaporn-katip/sms-email-ui/internal/smtpd"
	"github.com/akaporn-katip/sms-email-ui/internal/store"
	"github.com/akaporn-katip/sms-email-ui/web"
)

// Build metadata. The zero values are used for `go run` and local builds;
// release builds override them with -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if isVersionRequest(os.Args[1:]) {
		fmt.Printf("smsmail %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	if err := run(); err != nil {
		// -h / -help already printed the usage; that is not a failure.
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "smsmail:", err)
		os.Exit(1)
	}
}

// isVersionRequest reports whether the first argument asks for the version.
// It is checked before flag parsing so that `smsmail -version` works even when
// other flags would be invalid.
func isVersionRequest(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "-version", "--version", "-v":
			return true
		case "-help", "--help", "-h":
			return false
		}
	}
	return false
}

func run() error {
	cfg, err := config.Parse(os.Args[1:], os.Environ())
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	st := store.New(store.Config{
		InitialCredit:    cfg.Credit,
		AccountName:      cfg.Name,
		AccountEmail:     cfg.Email,
		RateLimitEnabled: cfg.RateLimit,
		FailureNumbers:   cfg.FailureNumbers,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 3)

	var smtpServer *smtp.Server
	if cfg.SMTPAddr != "" {
		smtpServer = smtpd.NewServer(cfg.SMTPAddr, st, logger)
		go func() {
			logger.Info("smtp catcher listening", "addr", cfg.SMTPAddr)
			if err := smtpServer.ListenAndServe(); err != nil && ctx.Err() == nil {
				errCh <- fmt.Errorf("smtp server: %w", err)
			}
		}()
	}

	apiHandler := smsapi.New(st, cfg, logger).Handler()

	var httpServers []*http.Server
	if cfg.APIAddr != "" {
		srv := &http.Server{
			Addr:              cfg.APIAddr,
			Handler:           apiHandler,
			ReadHeaderTimeout: 10 * time.Second,
		}
		httpServers = append(httpServers, srv)
		go func() {
			logger.Info("mock sms api listening", "addr", cfg.APIAddr)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("api server: %w", err)
			}
		}()
	}

	if cfg.WebAddr != "" {
		ui := web.New(st, cfg, logger, apiHandler)
		srv := &http.Server{
			Addr:              cfg.WebAddr,
			Handler:           ui,
			ReadHeaderTimeout: 10 * time.Second,
		}
		httpServers = append(httpServers, srv)
		go func() {
			logger.Info("web ui listening", "addr", cfg.WebAddr)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("web server: %w", err)
			}
		}()
	}

	printBanner(cfg)

	select {
	case err := <-errCh:
		stop()
		shutdown(httpServers, smtpServer, logger)
		return err
	case <-ctx.Done():
		fmt.Println()
		logger.Info("shutting down")
		shutdown(httpServers, smtpServer, logger)
		return nil
	}
}

func shutdown(servers []*http.Server, smtpServer *smtp.Server, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, srv := range servers {
		if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			logger.Warn("http shutdown", "addr", srv.Addr, "error", err)
		}
	}
	if smtpServer != nil {
		if err := smtpServer.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			logger.Warn("smtp shutdown", "error", err)
		}
	}
}

func printBanner(cfg config.Config) {
	var sb strings.Builder
	sb.WriteString("\n  smsmail is ready\n")
	sb.WriteString("  ────────────────────────────────────────────\n")
	fmt.Fprintf(&sb, "  Version       %s\n", version)
	if cfg.WebAddr != "" {
		fmt.Fprintf(&sb, "  Web UI        http://localhost%s\n", hostPort(cfg.WebAddr))
	}
	if cfg.APIAddr != "" {
		fmt.Fprintf(&sb, "  SMS API       http://localhost%s/api/v1\n", hostPort(cfg.APIAddr))
	}
	if cfg.WebAddr != "" {
		fmt.Fprintf(&sb, "  API via UI    http://localhost%s/api/v1\n", hostPort(cfg.WebAddr))
	}
	if cfg.SMTPAddr != "" {
		fmt.Fprintf(&sb, "  SMTP          localhost%s (no auth required)\n", hostPort(cfg.SMTPAddr))
	}
	fmt.Fprintf(&sb, "  Credit        %d SMS\n", cfg.Credit)
	fmt.Fprintf(&sb, "  Rate limit    %s\n", onOff(cfg.RateLimit))
	fmt.Fprintf(&sb, "  Fail numbers  %s\n", strings.Join(cfg.FailureNumbers, ", "))
	fmt.Fprintf(&sb, "\n  Send mail:    swaks --to you@example.com --server localhost:%s\n", strings.TrimPrefix(hostPort(cfg.SMTPAddr), ":"))
	fmt.Fprintf(&sb, "  Send SMS:     curl -s localhost%s/api/v1/balance -H 'Authorization: Bearer sk_test'\n", hostPort(cfg.APIAddr))
	sb.WriteString("  ────────────────────────────────────────────\n")
	fmt.Println(sb.String())
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

// hostPort renders an address as ":port" even when it was configured with a
// host, so that the banner can be prefixed with "localhost".
func hostPort(addr string) string {
	if addr == "" {
		return ""
	}
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return ":" + port
	}
	return addr
}
