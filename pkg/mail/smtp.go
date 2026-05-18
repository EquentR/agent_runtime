package mail

import (
	"context"
	"crypto/tls"
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
	client, err := smtp.Dial(s.addr)
	if err != nil {
		return fmt.Errorf("dial smtp: %w", err)
	}
	defer client.Close()

	if err := ctx.Err(); err != nil {
		return err
	}
	if s.config.UseStartTLS {
		tlsConfig := &tls.Config{ServerName: s.config.Host, MinVersion: tls.VersionTLS12}
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("smtp server does not support STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("start tls: %w", err)
		}
	}
	return s.sendWithClient(ctx, client, from, to, payload)
}

func (s *smtpSender) sendTLS(ctx context.Context, from string, to string, payload []byte) error {
	conn, err := tls.Dial("tcp", s.addr, &tls.Config{ServerName: s.config.Host, MinVersion: tls.VersionTLS12})
	if err != nil {
		return fmt.Errorf("dial smtp tls: %w", err)
	}
	client, err := smtp.NewClient(conn, s.config.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("create smtp tls client: %w", err)
	}
	defer client.Close()

	return s.sendWithClient(ctx, client, from, to, payload)
}

func (s *smtpSender) sendWithClient(ctx context.Context, client *smtp.Client, from string, to string, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	auth := smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := writer.Write(payload); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write smtp payload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close smtp payload: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("smtp quit: %w", err)
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
