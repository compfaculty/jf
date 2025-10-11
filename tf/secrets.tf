# Secrets Manager for sensitive configuration
resource "aws_secretsmanager_secret" "smtp_credentials" {
  name        = "${var.project_name}-${var.environment}-smtp-credentials"
  description = "SMTP credentials for email sending"

  tags = {
    Name = "${var.project_name}-${var.environment}-smtp-credentials"
  }
}

resource "aws_secretsmanager_secret_version" "smtp_credentials" {
  secret_id = aws_secretsmanager_secret.smtp_credentials.id
  secret_string = jsonencode({
    smtp_user     = var.smtp_user
    smtp_password = var.smtp_password
    llm_api_key   = var.llm_api_key
  })
}

