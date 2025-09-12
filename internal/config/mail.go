package config

type ResumeProfile struct {
	Name     string   `yaml:"name"`
	Patterns []string `yaml:"patterns"` // keywords/phrases to match in job title
	CVPath   string   `yaml:"cv_path"`
}

type MailConfig struct {
	SMTPHost  string          `yaml:"smtp_host"`
	SMTPPort  int             `yaml:"smtp_port"`
	SMTPUser  string          `yaml:"smtp_user"`
	SMTPPass  string          `yaml:"smtp_pass"`
	FromName  string          `yaml:"from_name"`
	FromEmail string          `yaml:"from_email"`
	BCC       string          `yaml:"bcc"`
	DefaultCV string          `yaml:"default_cv"`
	Profiles  []ResumeProfile `yaml:"profiles"`
}
