//go:build ignore

package emailx

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

// Send using Proton SMTP Submission (STARTTLS on 587)
// username = the custom-domain email you paired with the SMTP token
// password = the SMTP token you generated in Settings → Proton Mail → IMAP/SMTP → SMTP tokens
func SendProtonSMTP(username, password, to, subject, body string) error {
	const (
		host = "smtp.protonmail.ch"
		port = "587"
	)

	addr := net.JoinHostPort(host, port)

	// 1) Plain TCP, then issue STARTTLS
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("client: %w", err)
	}
	defer c.Quit()

	// 2) STARTTLS (required)
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return fmt.Errorf("server does not support STARTTLS")
	}
	if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
		return fmt.Errorf("starttls: %w", err)
	}

	// 3) Auth (PLAIN or LOGIN are supported; PLAIN works fine)
	auth := smtp.PlainAuth("", username, password, host)
	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	// 4) Build and send message
	if err := c.Mail(username); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	msg := strings.Builder{}
	msg.WriteString("From: " + username + "\r\n")
	msg.WriteString("To: " + to + "\r\n")
	msg.WriteString("Subject: " + subject + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body + "\r\n")

	if _, err := w.Write([]byte(msg.String())); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}

	return nil
}

func Send_SMTP_Submission_no_Bridge() {
	if err := SendProtonSMTP(
		"user@yourdomain.com",  // SMTP username (the custom-domain address)
		"your_smtp_token_here", // SMTP token (NOT your login password)
		"dest@example.com",
		"Hello from Go via Proton SMTP",
		"This is a test sent with STARTTLS on 587.",
	); err != nil {
		log.Fatal(err)
	}
	log.Println("sent")
}

func Read_IMAP_via_Proton_Mail_Bridge() {
	// Replace with the exact values Bridge shows you for IMAP.
	// Often hostPort looks like "127.0.0.1:1143" (STARTTLS).
	host := "127.0.0.1"
	hostPort := "127.0.0.1:1143"
	username := "your-bridge-username"
	password := "your-bridge-password"

	// 1) Plain TCP + STARTTLS (preferred)
	c, err := client.Dial(hostPort)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Logout()

	// STARTTLS — Bridge uses a local certificate; if validation fails on localhost,
	// you can omit ServerName or, as a last resort, set InsecureSkipVerify=true.
	if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
		log.Fatal("starttls:", err)
	}

	if err := c.Login(username, password); err != nil {
		log.Fatal("login:", err)
	}

	// 2) Select INBOX and fetch the last 10 messages' envelopes
	mbox, err := c.Select("INBOX", false)
	if err != nil {
		log.Fatal("select:", err)
	}
	if mbox.Messages == 0 {
		fmt.Println("INBOX empty")
		return
	}

	// Figure the sequence set for the last up-to-10 messages
	from := uint32(1)
	if mbox.Messages > 10 {
		from = mbox.Messages - 9
	}
	seqset := new(imap.SeqSet)
	seqset.AddRange(from, mbox.Messages)

	section := &imap.BodySectionName{} // not used here, but you can add BODY[] if needed
	items := []imap.FetchItem{imap.FetchEnvelope}

	messages := make(chan *imap.Message, 10)
	done := make(chan error, 1)
	go func() { done <- c.Fetch(seqset, items, messages) }()

	for msg := range messages {
		fmt.Printf("UID=%d | From=%s | Subject=%s | Date=%v\n",
			msg.Uid,
			msg.Envelope.From[0].MailboxName+"@"+msg.Envelope.From[0].HostName,
			msg.Envelope.Subject,
			msg.Envelope.Date)
	}
	if err := <-done; err != nil {
		log.Fatal("fetch:", err)
	}
}
