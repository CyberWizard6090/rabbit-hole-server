package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

const (
	SecurityNone     = "none"
	SecuritySTARTTLS = "starttls"
	SecurityTLS      = "tls"

	smtpTimeout = 30 * time.Second
)

type SMTPOptions struct {
	Host     string
	Port     int
	Username string
	Password string
	Security string
	From     string
}

type SMTP struct {
	opts SMTPOptions
	from string
}

func NewSMTP(opts SMTPOptions) (*SMTP, error) {
	from, err := parseAddress(opts.From)
	if err != nil {
		return nil, fmt.Errorf("MAIL_FROM: %w", err)
	}
	if opts.Host == "" {
		return nil, fmt.Errorf("%w: SMTP host is required", ErrInvalidMessage)
	}
	switch opts.Security {
	case SecurityNone, SecuritySTARTTLS, SecurityTLS:
	default:
		return nil, fmt.Errorf("%w: unknown SMTP security %q", ErrInvalidMessage, opts.Security)
	}
	return &SMTP{opts: opts, from: from.String()}, nil
}

func randomID() string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func domainOf(address string) string {
	at := strings.LastIndex(address, "@")
	if at < 0 {
		return "localhost"
	}
	return strings.TrimSuffix(address[at+1:], ">")
}

func (s *SMTP) build(to string, msg Message) ([]byte, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	write := func(contentType, content string) error {
		headers := textproto.MIMEHeader{}
		headers.Set("Content-Type", contentType+"; charset=utf-8")
		headers.Set("Content-Transfer-Encoding", "quoted-printable")
		part, err := writer.CreatePart(headers)
		if err != nil {
			return err
		}
		qp := quotedprintable.NewWriter(part)
		if _, err := qp.Write([]byte(content)); err != nil {
			return err
		}
		return qp.Close()
	}
	if err := write("text/plain", msg.Text); err != nil {
		return nil, err
	}
	if msg.HTML != "" {
		if err := write("text/html", msg.HTML); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	fmt.Fprintf(&out, "From: %s\r\n", s.from)
	fmt.Fprintf(&out, "To: %s\r\n", to)
	fmt.Fprintf(&out, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", msg.Subject))
	fmt.Fprintf(&out, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&out, "Message-ID: <%s@%s>\r\n", randomID(), domainOf(s.from))
	out.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&out, "Content-Type: multipart/alternative; boundary=%s\r\n\r\n", writer.Boundary())
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

func (s *SMTP) Send(ctx context.Context, msg Message) error {
	to, err := msg.validate()
	if err != nil {
		return err
	}
	from, err := parseAddress(s.from)
	if err != nil {
		return err
	}
	data, err := s.build(to.String(), msg)
	if err != nil {
		return fmt.Errorf("build message: %w", err)
	}

	addr := net.JoinHostPort(s.opts.Host, strconv.Itoa(s.opts.Port))
	dialer := &net.Dialer{Timeout: smtpTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("connect to the mail server: %w", err)
	}
	deadline := time.Now().Add(smtpTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)

	tlsConfig := &tls.Config{ServerName: s.opts.Host, MinVersion: tls.VersionTLS12}
	if s.opts.Security == SecurityTLS {
		conn = tls.Client(conn, tlsConfig)
	}
	client, err := smtp.NewClient(conn, s.opts.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("start the SMTP session: %w", err)
	}
	defer func() { _ = client.Close() }()

	if s.opts.Security == SecuritySTARTTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("the mail server does not offer STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if s.opts.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.opts.Username, s.opts.Password, s.opts.Host)); err != nil {
			return fmt.Errorf("SMTP authentication: %w", err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to.Address); err != nil {
		return fmt.Errorf("RCPT TO: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("finish message: %w", err)
	}
	return client.Quit()
}
