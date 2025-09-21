package emailx

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/mail"
)

// ---- Config helpers ----

func mustEnv(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		log.Fatalf("missing env %s", key)
	}
	return v
}

// normalize 16-char Google app password (allow spaces)
func normAppPass(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), " ", "")
}

// ---- SEND (SMTP over STARTTLS 587) ----

func sendGmailSMTP(from, appPass, to, subject, body string) error {
	const host = "smtp.gmail.com"
	const port = "587"
	addr := net.JoinHostPort(host, port)

	// 1) TCP connect
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	// 2) SMTP client
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer c.Quit()

	// 3) STARTTLS
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return errors.New("server does not support STARTTLS")
	}
	if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
		return fmt.Errorf("starttls: %w", err)
	}

	// 4) AUTH (PLAIN)
	auth := smtp.PlainAuth("", from, normAppPass(appPass), host)
	if err := c.Auth(auth); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	// 5) MAIL TX
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("rcpt to: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}

	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	b.WriteString("\r\n")

	if _, err := w.Write([]byte(b.String())); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close data: %w", err)
	}
	return nil
}

// ---- READ (IMAP over TLS 993) ----

func readGmailIMAP(user, appPass string, limit uint) error {
	const host = "imap.gmail.com:993"
	// 1) TLS dial
	c, err := client.DialTLS(host, &tls.Config{ServerName: "imap.gmail.com"})
	if err != nil {
		return fmt.Errorf("dial tls: %w", err)
	}
	defer c.Logout()

	// 2) LOGIN
	if err := c.Login(user, normAppPass(appPass)); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	// 3) Select mailbox
	mbox, err := c.Select("INBOX", true) // read-only
	if err != nil {
		return fmt.Errorf("select INBOX: %w", err)
	}
	if mbox.Messages == 0 {
		fmt.Println("INBOX empty")
		return nil
	}

	// Pick the last N messages
	var from uint32 = 1
	if mbox.Messages > uint32(limit) {
		from = mbox.Messages - uint32(limit) + 1
	}
	seqset := new(imap.SeqSet)
	seqset.AddRange(from, mbox.Messages)

	// We’ll fetch ENVELOPE and the full BODY[] then parse a snippet
	section := &imap.BodySectionName{}
	items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchBodyStructure, section.FetchItem()}

	msgCh := make(chan *imap.Message, limit)
	done := make(chan error, 1)
	go func() { done <- c.Fetch(seqset, items, msgCh) }()

	i := 0
	for msg := range msgCh {
		i++
		env := msg.Envelope
		fmt.Printf("\n[%d] UID=%d  Date=%v\n", i, msg.Uid, env.Date)
		fromAddr := "(unknown)"
		if len(env.From) > 0 {
			fromAddr = env.From[0].Address()
		}
		fmt.Printf("From: %s\n", fromAddr)
		fmt.Printf("Subj: %s\n", env.Subject)

		r := msg.GetBody(section)
		if r == nil {
			fmt.Println("(no body)")
			continue
		}
		snippet, err := firstTextSnippet(r, 400)
		if err != nil {
			fmt.Printf("(parse err: %v)\n", err)
			continue
		}
		fmt.Printf("Body: %s\n", snippet)
	}
	if err := <-done; err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	return nil
}

// Extract a short text/plain snippet from a message stream
func firstTextSnippet(r io.Reader, max int) (string, error) {
	mr, err := message.Read(r)
	if err != nil {
		return "", err
	}
	// If singlepart
	if mr.MultipartReader() == nil {
		mediatype, _, _ := mr.Header.ContentType()
		if strings.EqualFold(mediatype, "text/plain") || mediatype == "" {
			b, _ := io.ReadAll(io.LimitReader(mr.Body, int64(max)))
			return trimLines(string(b)), nil
		}
		// fallthrough: not text/plain
		return "", nil
	}
	// Multipart: walk parts to find text/plain first
	mp := mr.MultipartReader()
	for {
		p, perr := mp.NextPart()
		if perr == io.EOF {
			break
		}
		if perr != nil {
			return "", perr
		}
		ct := p.Header.Get("Content-Type")
		if strings.HasPrefix(strings.ToLower(ct), "text/plain") {
			b, _ := io.ReadAll(io.LimitReader(p.Body, int64(max)))
			return trimLines(string(b)), nil
		}
	}
	return "", nil
}

func trimLines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			out = append(out, ln)
		}
	}
	joined := strings.Join(out, " ")
	if len(joined) > 400 {
		joined = joined[:400] + "..."
	}
	return joined
}

// ---- CLI ----

func main() {
	log.SetFlags(0)

	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  send -to someone@example.com -subject 'Hi' -body 'Hello from Go'")
		fmt.Println("  read -n 10")
		os.Exit(2)
	}

	switch os.Args[1] {
	case "send":
		fs := flag.NewFlagSet("send", flag.ExitOnError)
		to := fs.String("to", "", "recipient email")
		sub := fs.String("subject", "(no subject)", "subject")
		body := fs.String("body", "", "plain text body")
		_ = fs.Parse(os.Args[2:])
		if *to == "" {
			log.Fatal("-to is required")
		}
		user := mustEnv("GMAIL_USER")
		pass := mustEnv("GMAIL_APP_PASSWORD")

		if err := sendGmailSMTP(user, pass, *to, *sub, *body); err != nil {
			log.Fatal("send:", err)
		}
		fmt.Println("OK: sent")

	case "read":
		fs := flag.NewFlagSet("read", flag.ExitOnError)
		n := fs.Uint("n", 5, "how many latest messages")
		_ = fs.Parse(os.Args[2:])

		user := mustEnv("GMAIL_USER")
		pass := mustEnv("GMAIL_APP_PASSWORD")
		if err := readGmailIMAP(user, pass, *n); err != nil {
			log.Fatal("read:", err)
		}

	default:
		log.Fatalf("unknown subcommand %q (use send/read)", os.Args[1])
	}
}
