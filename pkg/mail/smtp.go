package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
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
	config SMTPConfig
	addr   string
}

func (c SMTPConfig) ValidateForSend() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.Host) == "" {
		return fmt.Errorf("smtp host is required")
	}
	if strings.TrimSpace(c.From) == "" {
		return fmt.Errorf("smtp from is required")
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
	config.Host = strings.TrimSpace(config.Host)
	config.Username = strings.TrimSpace(config.Username)
	config.From = strings.TrimSpace(config.From)
	return &smtpSender{
		config: config,
		addr:   net.JoinHostPort(config.Host, fmt.Sprintf("%d", config.Port)),
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

	to := strings.TrimSpace(message.To)
	if to == "" {
		return fmt.Errorf("message recipient is required")
	}
	subject := strings.TrimSpace(message.Subject)
	body := message.Body
	payload := strings.Join([]string{
		"From: " + s.config.From,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")

	if s.config.UseTLS {
		return s.sendTLS(ctx, to, []byte(payload))
	}
	return s.sendPlain(ctx, to, []byte(payload))
}

func (s *smtpSender) sendPlain(ctx context.Context, to string, payload []byte) error {
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
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("start tls: %w", err)
			}
		}
	}
	return s.sendWithClient(ctx, client, to, payload)
}

func (s *smtpSender) sendTLS(ctx context.Context, to string, payload []byte) error {
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

	return s.sendWithClient(ctx, client, to, payload)
}

func (s *smtpSender) sendWithClient(ctx context.Context, client *smtp.Client, to string, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	auth := smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Mail(s.config.From); err != nil {
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
