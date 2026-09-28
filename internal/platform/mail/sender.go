// Package mail is a narrow platform abstraction for sending email. It
// exposes only what the codebase actually needs today — one message
// shape, one send operation — not a generic notification framework
// (templates, attachments, multiple channels).
package mail

import "context"

// Message is the minimal representation of an email this platform can
// send.
type Message struct {
	To      string
	Subject string
	Body    string
}

// Sender delivers a single email message. Implementations must never
// log Body — it may carry a security-sensitive secret such as an
// email-verification link.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}
