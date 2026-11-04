package mail

import (
	"bufio"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSMTP struct {
	listener net.Listener
	mu       sync.Mutex
	from     string
	rcpts    []string
	data     string
	refuse   string
	starttls bool
	conns    int
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{listener: l}
	t.Cleanup(func() { _ = l.Close() })
	go f.serve()
	return f
}

func (f *fakeSMTP) port() int { return f.listener.Addr().(*net.TCPAddr).Port }

func (f *fakeSMTP) serve() {
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conns++
		f.mu.Unlock()
		go f.handle(conn)
	}
}

func (f *fakeSMTP) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	say := func(line string) { _, _ = io.WriteString(conn, line+"\r\n") }
	say("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			if f.starttls {
				say("250-fake")
				say("250 STARTTLS")
			} else {
				say("250 fake")
			}
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			f.mu.Lock()
			f.from = strings.TrimSpace(line[len("MAIL FROM:"):])
			f.mu.Unlock()
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			rcpt := strings.TrimSpace(line[len("RCPT TO:"):])
			if f.refuse != "" && strings.Contains(rcpt, f.refuse) {
				say("550 no such user")
				continue
			}
			f.mu.Lock()
			f.rcpts = append(f.rcpts, rcpt)
			f.mu.Unlock()
			say("250 ok")
		case cmd == "DATA":
			say("354 go ahead")
			var data strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				data.WriteString(l)
			}
			f.mu.Lock()
			f.data = data.String()
			f.mu.Unlock()
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("500 what")
		}
	}
}

func (f *fakeSMTP) snapshot() (from string, rcpts []string, data string, conns int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.from, append([]string(nil), f.rcpts...), f.data, f.conns
}

func newTestSMTP(t *testing.T, f *fakeSMTP, security string) *SMTP {
	t.Helper()
	s, err := NewSMTP(SMTPOptions{
		Host: "127.0.0.1", Port: f.port(), Security: security, From: "Rabbit Hole <no-reply@example.test>",
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSMTPSendsAMultipartMessage(t *testing.T) {
	f := newFakeSMTP(t)
	s := newTestSMTP(t, f, SecurityNone)

	err := s.Send(context.Background(), Message{
		To: "Ann <ann@example.test>", Subject: "Сброс пароля",
		Text: "Откройте ссылку: https://app.example.test/reset?token=abc\nСсылка действует час.",
		HTML: `<p>Откройте <a href="https://app.example.test/reset?token=abc">ссылку</a></p>`,
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	from, rcpts, data, _ := f.snapshot()
	if from != "<no-reply@example.test>" || len(rcpts) != 1 || rcpts[0] != "<ann@example.test>" {
		t.Fatalf("envelope from=%q rcpts=%v", from, rcpts)
	}

	msg, err := mail.ReadMessage(strings.NewReader(data))
	if err != nil {
		t.Fatalf("parse message: %v\n%s", err, data)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subject != "Сброс пароля" {
		t.Fatalf("subject = %q (%v)", subject, err)
	}
	if got := msg.Header.Get("From"); !strings.Contains(got, "no-reply@example.test") {
		t.Fatalf("From = %q", got)
	}
	if got := msg.Header.Get("Message-ID"); !strings.HasSuffix(got, "@example.test>") {
		t.Fatalf("Message-ID = %q", got)
	}
	if msg.Header.Get("Date") == "" || msg.Header.Get("MIME-Version") != "1.0" {
		t.Fatalf("headers = %v", msg.Header)
	}

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("content type = %q (%v)", mediaType, err)
	}
	reader := multipart.NewReader(msg.Body, params["boundary"])
	var types, bodies []string
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		ct, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		types = append(types, ct)
		bodies = append(bodies, strings.ReplaceAll(string(decoded), "\r\n", "\n"))
	}
	if len(types) != 2 || types[0] != "text/plain" || types[1] != "text/html" {
		t.Fatalf("parts = %v", types)
	}
	if !strings.Contains(bodies[0], "Ссылка действует час.") || !strings.Contains(bodies[0], "token=abc") {
		t.Fatalf("text part = %q", bodies[0])
	}
	if !strings.Contains(bodies[1], "ссылку") {
		t.Fatalf("html part = %q", bodies[1])
	}
}

func TestSMTPRefusesHeaderInjectionBeforeConnecting(t *testing.T) {
	f := newFakeSMTP(t)
	s := newTestSMTP(t, f, SecurityNone)

	for name, msg := range map[string]Message{
		"subject":    {To: "ann@example.test", Subject: "Hi\r\nBcc: victim@example.test", Text: "x"},
		"to":         {To: "ann@example.test\r\nBcc: victim@example.test", Subject: "Hi", Text: "x"},
		"to bare":    {To: "ann@example.test\nBcc: v@example.test", Subject: "Hi", Text: "x"},
		"null byte":  {To: "ann@example.test", Subject: "Hi\x00", Text: "x"},
		"no text":    {To: "ann@example.test", Subject: "Hi", Text: "  "},
		"no subject": {To: "ann@example.test", Subject: "", Text: "x"},
		"bad to":     {To: "not an address", Subject: "Hi", Text: "x"},
	} {
		if err := s.Send(context.Background(), msg); !errors.Is(err, ErrInvalidMessage) {
			t.Errorf("%s: err = %v, want ErrInvalidMessage", name, err)
		}
	}
	if _, _, _, conns := f.snapshot(); conns != 0 {
		t.Fatalf("a rejected message opened %d connections", conns)
	}
}

func TestSMTPReportsRefusedRecipientsAndMissingSTARTTLS(t *testing.T) {
	f := newFakeSMTP(t)
	f.refuse = "gone@"
	s := newTestSMTP(t, f, SecurityNone)
	if err := s.Send(context.Background(), Message{To: "gone@example.test", Subject: "Hi", Text: "x"}); err == nil {
		t.Fatal("a refused recipient must be an error")
	}

	plain := newFakeSMTP(t)
	starttls := newTestSMTP(t, plain, SecuritySTARTTLS)
	err := starttls.Send(context.Background(), Message{To: "ann@example.test", Subject: "Hi", Text: "x"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("err = %v, want a STARTTLS error", err)
	}
	if _, _, data, _ := plain.snapshot(); data != "" {
		t.Fatal("the message was sent without the TLS the configuration demands")
	}
}

func TestSMTPConnectionFailure(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	s, err := NewSMTP(SMTPOptions{Host: "127.0.0.1", Port: port, Security: SecurityNone, From: "no-reply@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.Send(ctx, Message{To: "ann@example.test", Subject: "Hi", Text: "x"}); err == nil {
		t.Fatal("expected a connection error")
	}
}

func TestNewSMTPValidatesItsOptions(t *testing.T) {
	good := SMTPOptions{Host: "smtp.example.test", Port: 587, Security: SecuritySTARTTLS, From: "no-reply@example.test"}
	if _, err := NewSMTP(good); err != nil {
		t.Fatalf("valid options: %v", err)
	}
	for name, mutate := range map[string]func(*SMTPOptions){
		"no host":      func(o *SMTPOptions) { o.Host = "" },
		"bad from":     func(o *SMTPOptions) { o.From = "nope" },
		"line break":   func(o *SMTPOptions) { o.From = "a@example.test\r\nBcc: x@example.test" },
		"bad security": func(o *SMTPOptions) { o.Security = "maybe" },
	} {
		opts := good
		mutate(&opts)
		if _, err := NewSMTP(opts); err == nil {
			t.Errorf("%s: options were accepted", name)
		}
	}
}
