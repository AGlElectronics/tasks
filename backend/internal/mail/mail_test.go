package mail

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestNormalized(t *testing.T) {
	if c := (Config{Host: " mail ", From: "a@b"}).Normalized(); c.Port != 587 || c.Security != "starttls" || c.Host != "mail" {
		t.Fatalf("defaults: %+v", c)
	}
	if c := (Config{Host: "mail", Security: "TLS"}).Normalized(); c.Port != 465 || c.Security != "tls" {
		t.Fatalf("tls defaults: %+v", c)
	}
	if (Config{Host: "mail"}).Enabled() || !(Config{Host: "mail", From: "a@b"}).Enabled() {
		t.Fatal("Enabled needs host and from")
	}
}

func TestRenderEncodesHeaders(t *testing.T) {
	out := string(Render(Config{From: "tasks@example.org", FromName: "Tasks"},
		Message{To: "maya@example.org", Subject: "CREW-4 “Radios”\r\nBcc: evil@example.org", Body: "line 1\nline 2"},
		time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)))
	if strings.Contains(out, "\r\nBcc:") {
		t.Fatalf("subject must not inject headers:\n%s", out)
	}
	if !strings.Contains(out, "Subject: =?utf-8?q?") || !strings.Contains(out, "line 1\r\nline 2") {
		t.Fatalf("unexpected message:\n%s", out)
	}
	if !strings.Contains(out, "From: Tasks <tasks@example.org>") {
		t.Fatalf("from header:\n%s", out)
	}
}

func TestSendNotConfigured(t *testing.T) {
	if err := Send(context.Background(), Config{}); err != ErrNotConfigured {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
}

// fakeSMTP is a minimal plain SMTP server that records DATA payloads.
func fakeSMTP(t *testing.T) (addr string, got chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got = make(chan string, 10)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		write := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		write("220 fake ready")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				write("250 fake")
			case strings.HasPrefix(cmd, "DATA"):
				write("354 go")
				var b strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
					b.WriteString(l)
				}
				got <- b.String()
				write("250 queued")
			case strings.HasPrefix(cmd, "QUIT"):
				write("221 bye")
				return
			default:
				write("250 ok")
			}
		}
	}()
	return ln.Addr().String(), got
}

func TestSendPlain(t *testing.T) {
	addr, got := fakeSMTP(t)
	host, port, _ := net.SplitHostPort(addr)
	var p int
	for _, c := range port {
		p = p*10 + int(c-'0')
	}
	cfg := Config{Host: host, Port: p, From: "tasks@example.org", Security: "none"}
	err := Send(context.Background(), cfg,
		Message{To: "a@example.org", Subject: "one", Body: "first"},
		Message{To: "b@example.org", Subject: "two", Body: "second"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	for _, want := range []string{"first", "second"} {
		select {
		case m := <-got:
			if !strings.Contains(m, want) {
				t.Fatalf("want %q in %q", want, m)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no message")
		}
	}
	// starttls against a server without it is refused, not silently plain.
	addr2, _ := fakeSMTP(t)
	host2, port2, _ := net.SplitHostPort(addr2)
	p2 := 0
	for _, c := range port2 {
		p2 = p2*10 + int(c-'0')
	}
	if err := Send(context.Background(), Config{Host: host2, Port: p2, From: "x@y"}, Message{To: "a@b"}); err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("want a STARTTLS error, got %v", err)
	}
}
