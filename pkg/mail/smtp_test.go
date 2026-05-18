package mail

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestSMTPConfigValidationRequiresHostFromAndCredentialsWhenEnabled(t *testing.T) {
	disabled := SMTPConfig{}
	if err := disabled.ValidateForSend(); err != nil {
		t.Fatalf("disabled ValidateForSend() error = %v", err)
	}

	missingHost := SMTPConfig{
		Enabled:  true,
		Port:     587,
		Username: "smtp-user",
		Password: "smtp-password",
		From:     "noreply@example.com",
	}
	if err := missingHost.ValidateForSend(); err == nil {
		t.Fatal("missing host ValidateForSend() error = nil, want error")
	}

	missingFrom := SMTPConfig{
		Enabled:  true,
		Host:     "smtp.example.com",
		Port:     587,
		Username: "smtp-user",
		Password: "smtp-password",
	}
	if err := missingFrom.ValidateForSend(); err == nil {
		t.Fatal("missing from ValidateForSend() error = nil, want error")
	}

	missingCredentials := SMTPConfig{
		Enabled: true,
		Host:    "smtp.example.com",
		Port:    587,
		From:    "noreply@example.com",
	}
	if err := missingCredentials.ValidateForSend(); err == nil {
		t.Fatal("missing credentials ValidateForSend() error = nil, want error")
	}

	config := SMTPConfig{
		Enabled:     true,
		Host:        "smtp.example.com",
		Port:        587,
		Username:    "smtp-user",
		Password:    "smtp-password",
		From:        "noreply@example.com",
		UseStartTLS: true,
	}
	if err := config.ValidateForSend(); err != nil {
		t.Fatalf("valid ValidateForSend() error = %v", err)
	}
	invalidHighPort := config
	invalidHighPort.Port = 65536
	if err := invalidHighPort.ValidateForSend(); err == nil {
		t.Fatal("port 65536 ValidateForSend() error = nil, want error")
	}
	bothTLSModes := config
	bothTLSModes.UseTLS = true
	bothTLSModes.UseStartTLS = true
	if err := bothTLSModes.ValidateForSend(); err == nil {
		t.Fatal("UseTLS and UseStartTLS ValidateForSend() error = nil, want error")
	}
	sender, err := NewSMTPSender(config)
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}
	if sender == nil {
		t.Fatal("NewSMTPSender() = nil, want sender")
	}
	if _, ok := sender.(interface {
		Send(context.Context, Message) error
	}); !ok {
		t.Fatal("sender does not implement Sender")
	}
}

func TestNewSMTPSenderDisabledConfigNoops(t *testing.T) {
	sender, err := NewSMTPSender(SMTPConfig{})
	if err != nil {
		t.Fatalf("NewSMTPSender(disabled) error = %v", err)
	}
	if sender == nil {
		t.Fatal("NewSMTPSender(disabled) = nil, want sender")
	}
	if err := sender.Send(context.Background(), Message{}); err != nil {
		t.Fatalf("Send(disabled) error = %v", err)
	}
}

func TestNewSMTPSenderDisplayNameFromUsesRawEnvelopeAddress(t *testing.T) {
	addr, captures, closeServer := startCapturingSMTPServer(t)
	defer closeServer()

	_, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatalf("parse port %q: %v", portText, err)
	}

	sender, err := NewSMTPSender(SMTPConfig{
		Enabled:  true,
		Host:     "localhost",
		Port:     port,
		Username: "smtp-user",
		Password: "smtp-password",
		From:     "Agent Runtime <noreply@example.com>",
	})
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}
	if err := sender.Send(context.Background(), Message{To: "user@example.com", Subject: "Subject", Body: "body"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	select {
	case capture := <-captures:
		if capture.mailFrom != "MAIL FROM:<noreply@example.com>" {
			t.Fatalf("MAIL FROM command = %q, want raw mailbox address", capture.mailFrom)
		}
		if !strings.Contains(capture.data, "From: Agent Runtime <noreply@example.com>\r\n") {
			t.Fatalf("message data = %q, want display-name From header", capture.data)
		}
	case <-time.After(time.Second):
		t.Fatal("fake SMTP server did not capture message")
	}
}

func TestSMTPConfigValidationRejectsHeaderInjectionAndInvalidAddresses(t *testing.T) {
	base := SMTPConfig{
		Enabled:  true,
		Host:     "localhost",
		Port:     1,
		Username: "smtp-user",
		Password: "smtp-password",
		From:     "noreply@example.com",
	}

	invalidFrom := base
	invalidFrom.From = "not an address"
	if err := invalidFrom.ValidateForSend(); err == nil {
		t.Fatal("invalid from ValidateForSend() error = nil, want error")
	}

	injectedFrom := base
	injectedFrom.From = "noreply@example.com\r\nBCC: attacker@example.com"
	if err := injectedFrom.ValidateForSend(); err == nil {
		t.Fatal("injected from ValidateForSend() error = nil, want error")
	}

	sender, err := NewSMTPSender(base)
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}
	if err := sender.Send(context.Background(), Message{To: "victim@example.com\r\nBCC: attacker@example.com", Subject: "Hello", Body: "body"}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "recipient") {
		t.Fatalf("Send(injected recipient) error = %v, want recipient validation error", err)
	}
	if err := sender.Send(context.Background(), Message{To: "victim@example.com", Subject: "Hello\r\nBCC: attacker@example.com", Body: "body"}); err == nil || !strings.Contains(strings.ToLower(err.Error()), "subject") {
		t.Fatalf("Send(injected subject) error = %v, want subject validation error", err)
	}
}

func TestSMTPConfigStartTLSRequiresServerSupport(t *testing.T) {
	addr, done, closeServer := startSMTPServerWithoutStartTLS(t)
	defer closeServer()

	_, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatalf("parse port %q: %v", portText, err)
	}

	sender, err := NewSMTPSender(SMTPConfig{
		Enabled:     true,
		Host:        "localhost",
		Port:        port,
		Username:    "smtp-user",
		Password:    "smtp-password",
		From:        "noreply@example.com",
		UseStartTLS: true,
	})
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}

	err = sender.Send(context.Background(), Message{To: "user@example.com", Subject: "Subject", Body: "body"})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "starttls") {
		t.Fatalf("Send() error = %v, want STARTTLS support error", err)
	}

	select {
	case serverErr := <-done:
		if serverErr != nil {
			t.Fatalf("fake SMTP server error = %v", serverErr)
		}
	case <-time.After(time.Second):
		t.Fatal("fake SMTP server did not finish")
	}
}

func TestSMTPSenderContextCanceledBeforeSendReturnsCanceled(t *testing.T) {
	sender, err := NewSMTPSender(SMTPConfig{
		Enabled:  true,
		Host:     "localhost",
		Port:     1,
		Username: "smtp-user",
		Password: "smtp-password",
		From:     "noreply@example.com",
	})
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = sender.Send(ctx, Message{To: "user@example.com", Subject: "Subject", Body: "body"})
	if err != context.Canceled {
		t.Fatalf("Send(canceled) error = %v, want context.Canceled", err)
	}
}

func TestSMTPSenderContextCancellationUnblocksSMTPDial(t *testing.T) {
	addr, closeServer := startSMTPServerWithoutGreeting(t)
	defer closeServer()

	_, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	var port int
	if _, err := fmt.Sscanf(portText, "%d", &port); err != nil {
		t.Fatalf("parse port %q: %v", portText, err)
	}
	sender, err := NewSMTPSender(SMTPConfig{
		Enabled:  true,
		Host:     "localhost",
		Port:     port,
		Username: "smtp-user",
		Password: "smtp-password",
		From:     "noreply@example.com",
	})
	if err != nil {
		t.Fatalf("NewSMTPSender() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- sender.Send(ctx, Message{To: "user@example.com", Subject: "Subject", Body: "body"})
	}()

	select {
	case err := <-errCh:
		if err != context.DeadlineExceeded {
			t.Fatalf("Send(timeout) error = %v, want context deadline exceeded", err)
		}
	case <-time.After(500 * time.Millisecond):
		closeServer()
		err := <-errCh
		t.Fatalf("Send(timeout) did not return before server close; final error = %v", err)
	}
}

func startSMTPServerWithoutStartTLS(t *testing.T) (string, <-chan error, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- nil
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		if _, err := fmt.Fprint(conn, "220 localhost ESMTP\r\n"); err != nil {
			done <- err
			return
		}
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				done <- nil
				return
			}
			command := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(command, "EHLO"):
				if _, err := fmt.Fprint(conn, "250-localhost\r\n250 AUTH PLAIN\r\n"); err != nil {
					done <- err
					return
				}
			case strings.HasPrefix(command, "AUTH"):
				done <- fmt.Errorf("AUTH attempted without STARTTLS")
				_, _ = fmt.Fprint(conn, "535 authentication rejected\r\n")
				return
			case strings.HasPrefix(command, "QUIT"):
				_, _ = fmt.Fprint(conn, "221 bye\r\n")
				done <- nil
				return
			default:
				if _, err := fmt.Fprint(conn, "250 ok\r\n"); err != nil {
					done <- err
					return
				}
			}
		}
	}()

	return listener.Addr().String(), done, func() { _ = listener.Close() }
}

func startSMTPServerWithoutGreeting(t *testing.T) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	done := make(chan struct{})
	connCh := make(chan net.Conn, 1)
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		connCh <- conn
		_, _ = bufio.NewReader(conn).ReadString('\n')
	}()

	closeServer := func() {
		_ = listener.Close()
		select {
		case conn := <-connCh:
			_ = conn.Close()
		default:
		}
		<-done
	}
	return listener.Addr().String(), closeServer
}

type smtpCapture struct {
	mailFrom string
	data     string
}

func startCapturingSMTPServer(t *testing.T) (string, <-chan smtpCapture, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	captures := make(chan smtpCapture, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		if _, err := fmt.Fprint(conn, "220 localhost ESMTP\r\n"); err != nil {
			return
		}
		var capture smtpCapture
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			command := strings.TrimSpace(line)
			upperCommand := strings.ToUpper(command)
			switch {
			case strings.HasPrefix(upperCommand, "EHLO"):
				if _, err := fmt.Fprint(conn, "250-localhost\r\n250 AUTH PLAIN\r\n"); err != nil {
					return
				}
			case strings.HasPrefix(upperCommand, "AUTH"):
				if _, err := fmt.Fprint(conn, "235 authenticated\r\n"); err != nil {
					return
				}
			case strings.HasPrefix(upperCommand, "MAIL FROM:"):
				capture.mailFrom = command
				if _, err := fmt.Fprint(conn, "250 ok\r\n"); err != nil {
					return
				}
			case strings.HasPrefix(upperCommand, "RCPT TO:"):
				if _, err := fmt.Fprint(conn, "250 ok\r\n"); err != nil {
					return
				}
			case upperCommand == "DATA":
				if _, err := fmt.Fprint(conn, "354 end with dot\r\n"); err != nil {
					return
				}
				var builder strings.Builder
				for {
					dataLine, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if strings.TrimSpace(dataLine) == "." {
						break
					}
					builder.WriteString(dataLine)
				}
				capture.data = builder.String()
				captures <- capture
				if _, err := fmt.Fprint(conn, "250 accepted\r\n"); err != nil {
					return
				}
			case upperCommand == "QUIT":
				_, _ = fmt.Fprint(conn, "221 bye\r\n")
				return
			default:
				if _, err := fmt.Fprint(conn, "250 ok\r\n"); err != nil {
					return
				}
			}
		}
	}()

	return listener.Addr().String(), captures, func() { _ = listener.Close() }
}
