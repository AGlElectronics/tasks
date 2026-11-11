// Package mail sends Tasks' email notifications over SMTP. Where the SMTP
// settings come from (Den or Tasks' own environment) is the caller's
// business; this package only delivers.
package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Config is where and as whom to send.
type Config struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	FromName string `json:"from_name"`
	// Security is starttls (default), tls (implicit TLS) or none.
	Security string `json:"security"`
}

// Enabled reports whether there is a server and a sender.
func (c Config) Enabled() bool { return strings.TrimSpace(c.Host) != "" && strings.TrimSpace(c.From) != "" }

// Normalized fills defaults: port 587 (465 for tls), security starttls.
func (c Config) Normalized() Config {
	c.Host = strings.TrimSpace(c.Host)
	c.Security = strings.ToLower(strings.TrimSpace(c.Security))
	if c.Security == "" {
		c.Security = "starttls"
	}
	if c.Port == 0 {
		c.Port = 587
		if c.Security == "tls" {
			c.Port = 465
		}
	}
	return c
}

// Message is a plain-text email.
type Message struct {
	To      string
	Subject string
	Body    string
}

// ErrNotConfigured is returned when there is no SMTP server to send with.
var ErrNotConfigured = errors.New("email is not configured")

// Send delivers the messages over one connection. It returns the first
// error; messages before it were delivered.
func Send(ctx context.Context, cfg Config, msgs ...Message) error {
	cfg = cfg.Normalized()
	if !cfg.Enabled() {
		return ErrNotConfigured
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	d := net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	var err error
	if cfg.Security == "tls" {
		conn, err = (&tls.Dialer{NetDialer: &d, Config: &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))
	}
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp hello: %w", err)
	}
	defer c.Close()
	if cfg.Security == "starttls" {
		ok, _ := c.Extension("STARTTLS")
		if !ok {
			return errors.New("smtp: the server does not offer STARTTLS (set security to tls or, for a local test server only, none)")
		}
		if err := c.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	for _, m := range msgs {
		if err := sendOne(c, cfg, m); err != nil {
			return err
		}
	}
	return c.Quit()
}

func sendOne(c *smtp.Client, cfg Config, m Message) error {
	if strings.ContainsAny(m.To, "\r\n") {
		return fmt.Errorf("smtp: invalid recipient %q", m.To)
	}
	if err := c.Mail(cfg.From); err != nil {
		return fmt.Errorf("smtp from: %w", err)
	}
	if err := c.Rcpt(m.To); err != nil {
		return fmt.Errorf("smtp to %s: %w", m.To, err)
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(Render(cfg, m, time.Now())); err != nil {
		return err
	}
	return w.Close()
}

// Render builds the RFC 5322 message. Header values are encoded, so a task
// title cannot inject headers.
func Render(cfg Config, m Message, now time.Time) []byte {
	from := cfg.From
	if cfg.FromName != "" {
		from = fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", cfg.FromName), cfg.From)
	}
	subject := strings.NewReplacer("\r", " ", "\n", " ").Replace(m.Subject)
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", m.To)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", now.Format(time.RFC1123Z))
	b.WriteString("Auto-Submitted: auto-generated\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	body := strings.ReplaceAll(strings.ReplaceAll(m.Body, "\r\n", "\n"), "\n", "\r\n")
	// A line with a single dot ends DATA; the smtp package dot-stuffs, but
	// keep bare CRs out.
	b.WriteString(strings.ReplaceAll(body, "\r\r", "\r"))
	return b.Bytes()
}
