package mail_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSMTPServer is a minimal, single-purpose SMTP server good enough
// to exercise SMTPSender's happy path and a handful of failure modes
// deterministically and without Docker/Mailpit. It is not a general
// SMTP implementation.
type fakeSMTPServer struct {
	listener net.Listener

	mu           sync.Mutex
	received     []receivedMessage
	authAttempts []string

	rejectRcpt bool
	rejectData bool
	delayMail  time.Duration
}

type receivedMessage struct {
	from string
	to   string
	data string
}

func newFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	s := &fakeSMTPServer{listener: ln}

	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })

	return s
}

func (s *fakeSMTPServer) addr() (string, int) {
	tcpAddr := s.listener.Addr().(*net.TCPAddr)

	return tcpAddr.IP.String(), tcpAddr.Port
}

func (s *fakeSMTPServer) messages() []receivedMessage {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]receivedMessage(nil), s.received...)
}

func (s *fakeSMTPServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}

		go s.handle(conn)
	}
}

func (s *fakeSMTPServer) handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	respond := func(line string) bool {
		if _, err := writer.WriteString(line + "\r\n"); err != nil {
			return false
		}
		return writer.Flush() == nil
	}

	if !respond("220 fake.smtp ESMTP") {
		return
	}

	var from, to string

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(upper, "EHLO"):
			// A real multi-line EHLO response: go-mail inspects advertised
			// extensions before deciding how to authenticate and check liveness.
			if !respond("250-fake.smtp") || !respond("250-AUTH PLAIN") || !respond("250 8BITMIME") {
				return
			}

		case strings.HasPrefix(upper, "HELO"):
			if !respond("250 fake.smtp") {
				return
			}

		case upper == "NOOP":
			if !respond("250 OK") {
				return
			}

		case upper == "RSET":
			if !respond("250 OK") {
				return
			}

		case strings.HasPrefix(upper, "AUTH PLAIN"):
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				if decoded, err := base64.StdEncoding.DecodeString(fields[2]); err == nil {
					s.mu.Lock()
					s.authAttempts = append(s.authAttempts, string(decoded))
					s.mu.Unlock()
				}
			}
			if !respond("235 authentication successful") {
				return
			}

		case strings.HasPrefix(upper, "MAIL FROM:"):
			if s.delayMail > 0 {
				time.Sleep(s.delayMail)
			}
			from = line[len("MAIL FROM:"):]
			if !respond("250 OK") {
				return
			}

		case strings.HasPrefix(upper, "RCPT TO:"):
			to = line[len("RCPT TO:"):]
			if s.rejectRcpt {
				if !respond("550 no such recipient") {
					return
				}
				continue
			}
			if !respond("250 OK") {
				return
			}

		case upper == "DATA":
			if s.rejectData {
				if !respond("554 transaction failed") {
					return
				}
				continue
			}

			if !respond("354 send data, end with <CRLF>.<CRLF>") {
				return
			}

			var data strings.Builder

			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dataLine, "\r\n") == "." {
					break
				}
				data.WriteString(dataLine)
			}

			s.mu.Lock()
			s.received = append(s.received, receivedMessage{from: from, to: to, data: data.String()})
			s.mu.Unlock()

			if !respond("250 message accepted") {
				return
			}

		case upper == "QUIT":
			if !respond("221 bye") {
				return
			}

			return

		default:
			if !respond("500 unrecognized command") {
				return
			}
		}
	}
}
