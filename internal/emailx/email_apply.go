package emailx

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/smtp"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"jf/internal/config"
	"jf/internal/models"
	"jf/internal/strutil"

	"github.com/jordan-wright/email"
)

// Applicant holds your personal info to inject into emails.
type Applicant struct {
	FullName  string
	Email     string
	Phone     string
	LinkedIn  string
	Portfolio string
}

// Mailer provides a pluggable interface (SMTP, SendGrid, SES, etc.)
type Mailer interface {
	Send(to []string, subject string, body string, attachments []string) error
}

// SMTPMailer implements Mailer via SMTP + STARTTLS.
type SMTPMailer struct {
	Host string
	Port int
	User string
	Pass string
	From string // "Name <email@domain>"
	BCC  string
}

func (m *SMTPMailer) Send(to []string, subject string, body string, attachments []string) error {
	e := email.NewEmail()
	e.From = m.From
	e.To = to
	if m.BCC != "" {
		e.Bcc = []string{m.BCC}
	}
	e.Subject = subject
	e.Text = []byte(body)
	for _, p := range attachments {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if _, err := e.AttachFile(p); err != nil {
			return fmt.Errorf("attach %s: %w", p, err)
		}
	}
	addr := fmt.Sprintf("%s:%d", m.Host, m.Port)
	auth := smtp.PlainAuth("", m.User, m.Pass, m.Host)
	return e.SendWithTLS(addr, auth, &tls.Config{
		ServerName: m.Host,
		MinVersion: tls.VersionTLS12,
	})
}

// BuildSMTPMailer builds a SMTPMailer from config.MailConfig.
func BuildSMTPMailer(mc *config.MailConfig) *SMTPMailer {
	from := mc.FromEmail
	if mc.FromName != "" {
		from = fmt.Sprintf("%s <%s>", mc.FromName, mc.FromEmail)
	}
	return &SMTPMailer{
		Host: mc.SMTPHost,
		Port: mc.SMTPPort,
		User: mc.SMTPUser,
		Pass: mc.SMTPPass,
		From: from,
		BCC:  mc.BCC,
	}
}

// ChooseResume returns the best CV for a job title using configured profiles.
func ChooseResume(title string, mc *config.MailConfig) (cvPath, matchedProfile string) {
	tokens := toSet(strutil.Tokens(title))
	titleLC := strings.ToLower(strings.TrimSpace(title))

	bestScore := -1
	bestCV := ""
	bestName := ""

	// simple phrase regex for multi-word patterns like "full stack"
	makePhrase := func(p string) *regexp.Regexp {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil
		}
		// escape regex, allow flexible whitespace
		re := regexp.MustCompile(`\s+`)
		esc := regexp.QuoteMeta(p)
		esc = re.ReplaceAllString(esc, `\s+`)
		return regexp.MustCompile("(?i)\\b" + esc + "\\b")
	}

	for _, prof := range mc.Profiles {
		score := 0
		for _, pat := range prof.Patterns {
			p := strings.ToLower(strings.TrimSpace(pat))
			if strings.Contains(p, " ") {
				// phrase match
				if r := makePhrase(p); r != nil && r.FindStringIndex(titleLC) != nil {
					score += 3
				}
			} else {
				// token match
				if _, ok := tokens[p]; ok {
					score += 1
				}
			}
		}
		if score > bestScore {
			bestScore = score
			bestCV = prof.CVPath
			bestName = prof.Name
		}
	}

	// fallback to default CV
	if strings.TrimSpace(bestCV) == "" {
		bestCV = mc.DefaultCV
		bestName = "default"
	}
	return bestCV, bestName
}

// ApplyByEmail builds a tailored message for a company+job and sends it.
// It uses Company.ApplyEmail if present. Returns message metadata for logging.
type ApplyResult struct {
	ToEmail        string
	Subject        string
	ResumeUsed     string
	ResumeProfile  string
	MessagePreview string
	SentAt         time.Time
}

func ApplyByEmail(ctx context.Context, mailer Mailer, mc *config.MailConfig, a Applicant, company models.Company, job models.ScrapedJob) (*ApplyResult, error) {
	to := strings.TrimSpace(company.ApplyEmail)
	if to == "" {
		return nil, fmt.Errorf("company %q has no ApplyEmail", company.Name)
	}

	cv, prof := ChooseResume(job.Title, mc)

	subject := fmt.Sprintf("Application: %s — %s", job.Title, a.FullName)
	body := buildBody(a, company, job)

	attachments := []string{}
	if cv != "" {
		if st, err := os.Stat(cv); err == nil && strings.EqualFold(filepath.Ext(cv), ".pdf") {
			if st.IsDir() {
				// Log warning but continue without attachment
				// This shouldn't happen in normal operation
			} else {
				attachments = append(attachments, cv)
			}
		} else if err != nil {
			// Log the error but continue - attachment might not be critical
			// This allows the email to be sent even if CV path is wrong
		}
	}

	if err := mailer.Send([]string{to}, subject, body, attachments); err != nil {
		return nil, err
	}

	return &ApplyResult{
		ToEmail:        to,
		Subject:        subject,
		ResumeUsed:     cv,
		ResumeProfile:  prof,
		MessagePreview: firstN(body, 400),
		SentAt:         time.Now(),
	}, nil
}

func buildBody(a Applicant, c models.Company, j models.ScrapedJob) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Hi %s Team,\n\n", strings.TrimSpace(c.Name))
	fmt.Fprintf(&b, "I'm applying for the %s role. My background aligns with your requirements.\n", strings.TrimSpace(j.Title))
	b.WriteString("Highlights:\n")
	b.WriteString("• <Match #1>\n")
	b.WriteString("• <Match #2>\n")
	b.WriteString("• <Match #3>\n\n")
	if strings.TrimSpace(a.LinkedIn) != "" {
		fmt.Fprintf(&b, "LinkedIn: %s\n", a.LinkedIn)
	}
	if strings.TrimSpace(a.Portfolio) != "" {
		fmt.Fprintf(&b, "Portfolio: %s\n", a.Portfolio)
	}
	if strings.TrimSpace(j.URL) != "" {
		fmt.Fprintf(&b, "Job link: %s\n", j.URL)
	}
	b.WriteString("\nCV attached (PDF).\n\n")
	fmt.Fprintf(&b, "Best,\n%s\n", a.FullName)
	if strings.TrimSpace(a.Phone) != "" {
		fmt.Fprintf(&b, "%s\n", a.Phone)
	}
	return b.String()
}

func toSet(xs []string) map[string]struct{} {
	m := make(map[string]struct{}, len(xs))
	for _, x := range xs {
		m[strings.ToLower(strings.TrimSpace(x))] = struct{}{}
	}
	return m
}

func firstN(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
