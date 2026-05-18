package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	netmail "net/mail"
	"net/smtp"
	"strings"
)

type SMTPConfig struct {
	Enabled     bool
	Host        string
	Port        int
	Username    string
	Password    string
	From        string
	UseTLS      bool
	UseStartTLS bool
}

type Message struct {
	To      string
	Subject string
	Body    string
}

type Sender interface {
	Send(ctx context.Context, message Message) error
}

type smtpSender struct {
	config       SMTPConfig
	addr         string
	fromHeader   string
	envelopeFrom string
}

type smtpPreparedMessage struct {
	envelopeFrom string
	envelopeTo   string
	payload      []byte
}

func (c SMTPConfig) ValidateForSend() error {
	if !c.Enabled {
		return nil
	}
	if c.UseTLS && c.UseStartTLS {
		return fmt.Errorf("smtp useTLS and useStartTLS are mutually exclusive")
	}
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("smtp host is required")
	}
	if strings.TrimSpace(c.From) == "" {
		return fmt.Errorf("smtp from is required")
	}
	if _, err := validateEmailAddress(c.From, "smtp from"); err != nil {
		return err
	}
	if strings.TrimSpace(c.Username) == "" || c.Password == "" {
		return fmt.Errorf("smtp username and password are required")
	}
	if c.Port <= 0 {
		return fmt.Errorf("smtp port is required")
	}
	return nil
}

func NewSMTPSender(config SMTPConfig) (Sender, error) {
	if err := config.ValidateForSend(); err != nil {
		return nil, err
	}
	if !config.Enabled {
		return &smtpSender{config: config}, nil
	}
	config.Host = strings.TrimSpace(config.Host)
	config.Username = strings.TrimSpace(config.Username)
	fromHeader, from, err := parseEmailAddress(config.From, "smtp from")
	if err != nil {
		return nil, err
	}
	config.From = fromHeader
	return &smtpSender{
		config:       config,
		addr:         net.JoinHostPort(config.Host, fmt.Sprintf("%d", config.Port)),
		fromHeader:   fromHeader,
		envelopeFrom: from.Address,
	}, nil
}

func (s *smtpSender) Send(ctx context.Context, message Message) error {
	if s == nil {
		return fmt.Errorf("smtp sender is not configured")
	}
	if !s.config.Enabled {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	prepared, err := s.prepareMessage(message)
	if err != nil {
		return err
	}

	if s.config.UseTLS {
		return s.sendTLS(ctx, prepared.envelopeFrom, prepared.envelopeTo, prepared.payload)
	}
	return s.sendPlain(ctx, prepared.envelopeFrom, prepared.envelopeTo, prepared.payload)
}

func (s *smtpSender) prepareMessage(message Message) (smtpPreparedMessage, error) {
	toHeader, to, err := parseEmailAddress(message.To, "message recipient")
	if err != nil {
		return smtpPreparedMessage{}, err
	}
	subject, err := validateHeaderValue(message.Subject, "message subject")
	if err != nil {
		return smtpPreparedMessage{}, err
	}
	body := message.Body
	fromHeader := s.fromHeader
	if fromHeader == "" {
		fromHeader = s.config.From
	}
	payload := strings.Join([]string{
		"From: " + fromHeader,
		"To: " + toHeader,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")

	envelopeFrom := s.envelopeFrom
	if envelopeFrom == "" {
		_, from, err := parseEmailAddress(s.config.From, "smtp from")
		if err != nil {
			return smtpPreparedMessage{}, err
		}
		envelopeFrom = from.Address
	}
	return smtpPreparedMessage{
		envelopeFrom: envelopeFrom,
		envelopeTo:   to.Address,
		payload:      []byte(payload),
	}, nil
}

func (s *smtpSender) sendPlain(ctx context.Context, from string, to string, payload []byte) error {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return wrapSMTPError("dial smtp", err)
	}
	client, err := newSMTPClientWithContext(ctx, conn, s.config.Host)
	if err != nil {
		_ = conn.Close()
		return wrapSMTPError("dial smtp", err)
	}
	defer client.Close()

	if err := ctx.Err(); err != nil {
		return err
	}
	if s.config.UseStartTLS {
		tlsConfig := &tls.Config{ServerName: s.config.Host, MinVersion: tls.VersionTLS12}
		ok, _, err := smtpExtension(ctx, client, "STARTTLS")
		if err != nil {
			return wrapSMTPError("smtp extension STARTTLS", err)
		}
		if !ok {
			return fmt.Errorf("smtp server does not support STARTTLS")
		}
		if err := runSMTPCommand(ctx, client.Close, func() error {
			return client.StartTLS(tlsConfig)
		}); err != nil {
			return wrapSMTPError("start tls", err)
		}
	}
	return s.sendWithClient(ctx, client, from, to, payload)
}

func (s *smtpSender) sendTLS(ctx context.Context, from string, to string, payload []byte) error {
	dialer := tls.Dialer{NetDialer: &net.Dialer{}, Config: &tls.Config{ServerName: s.config.Host, MinVersion: tls.VersionTLS12}}
	conn, err := dialer.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return wrapSMTPError("dial smtp tls", err)
	}
	client, err := newSMTPClientWithContext(ctx, conn, s.config.Host)
	if err != nil {
		_ = conn.Close()
		return wrapSMTPError("create smtp tls client", err)
	}
	defer client.Close()

	return s.sendWithClient(ctx, client, from, to, payload)
}

func (s *smtpSender) sendWithClient(ctx context.Context, client *smtp.Client, from string, to string, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	auth := smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)
	if err := runSMTPCommand(ctx, client.Close, func() error {
		return client.Auth(auth)
	}); err != nil {
		return wrapSMTPError("smtp auth", err)
	}
	if err := runSMTPCommand(ctx, client.Close, func() error {
		return client.Mail(from)
	}); err != nil {
		return wrapSMTPError("smtp mail from", err)
	}
	if err := runSMTPCommand(ctx, client.Close, func() error {
		return client.Rcpt(to)
	}); err != nil {
		return wrapSMTPError("smtp rcpt", err)
	}
	var writerCloser interface {
		Write([]byte) (int, error)
		Close() error
	}
	if err := runSMTPCommand(ctx, client.Close, func() error {
		var err error
		writerCloser, err = client.Data()
		return err
	}); err != nil {
		return wrapSMTPError("smtp data", err)
	}
	if err := runSMTPCommand(ctx, client.Close, func() error {
		_, err := writerCloser.Write(payload)
		return err
	}); err != nil {
		_ = writerCloser.Close()
		return wrapSMTPError("write smtp payload", err)
	}
	if err := runSMTPCommand(ctx, client.Close, writerCloser.Close); err != nil {
		return wrapSMTPError("close smtp payload", err)
	}
	if err := runSMTPCommand(ctx, client.Close, client.Quit); err != nil {
		return wrapSMTPError("smtp quit", err)
	}
	return nil
}

func newSMTPClientWithContext(ctx context.Context, conn net.Conn, host string) (*smtp.Client, error) {
	type result struct {
		client *smtp.Client
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		client, err := smtp.NewClient(conn, host)
		resultCh <- result{client: client, err: err}
	}()

	select {
	case result := <-resultCh:
		return result.client, result.err
	case <-ctx.Done():
		_ = conn.Close()
		result := <-resultCh
		if result.client != nil {
			_ = result.client.Close()
		}
		return nil, ctx.Err()
	}
}

func smtpExtension(ctx context.Context, client *smtp.Client, extension string) (bool, string, error) {
	type result struct {
		ok    bool
		param string
	}
	var extensionResult result
	if err := runSMTPCommand(ctx, client.Close, func() error {
		extensionResult.ok, extensionResult.param = client.Extension(extension)
		return nil
	}); err != nil {
		return false, "", err
	}
	return extensionResult.ok, extensionResult.param, nil
}

func runSMTPCommand(ctx context.Context, closeFn func() error, fn func() error) error {
	if err := ctx.Err(); err != nil {
		if closeFn != nil {
			_ = closeFn()
		}
		return err
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- fn()
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		if closeFn != nil {
			_ = closeFn()
		}
		return ctx.Err()
	}
}

func wrapSMTPError(operation string, err error) error {
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

func validateEmailAddress(value, field string) (*netmail.Address, error) {
	_, address, err := parseEmailAddress(value, field)
	if err != nil {
		return nil, err
	}
	return address, nil
}

func parseEmailAddress(value, field string) (string, *netmail.Address, error) {
	value, err := validateHeaderValue(value, field)
	if err != nil {
		return "", nil, err
	}
	address, err := netmail.ParseAddress(value)
	if err != nil {
		return "", nil, fmt.Errorf("%s is invalid: %w", field, err)
	}
	return value, address, nil
}

func validateHeaderValue(value, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	if strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("%s must not contain line breaks", field)
	}
	return value, nil
}
