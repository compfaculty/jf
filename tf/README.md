# Terraform Infrastructure for JF Application

This Terraform configuration deploys the JF job scraping application to AWS using ECS Fargate with a complete production-ready infrastructure.

## Architecture Overview

The infrastructure includes:

- **VPC**: Custom VPC with public and private subnets across 2 availability zones
- **ECS Fargate**: Serverless container orchestration for running the application
- **ECR**: Container registry for Docker images
- **EFS**: Elastic File System for persistent data storage (jobs database, PDFs)
- **ALB**: Application Load Balancer with health checks
- **CloudWatch**: Logs, metrics, and alarms for monitoring
- **Secrets Manager**: Secure storage for sensitive credentials (SMTP, API keys)
- **IAM**: Least-privilege roles and policies for ECS tasks
- **Auto Scaling**: CPU and memory-based auto-scaling (2-10 tasks)
- **NAT Gateways**: For outbound internet access from private subnets

## Prerequisites

1. **AWS Account** with appropriate permissions
2. **AWS CLI** configured with credentials:
   ```bash
   aws configure
   ```
3. **Terraform** >= 1.0 installed:
   ```bash
   # Download from https://www.terraform.io/downloads
   ```
4. **Docker** for building and pushing images

## Quick Start

### 1. Configure Variables

Copy the example variables file and customize it:

```bash
cd tf
cp terraform.tfvars.example terraform.tfvars
```

Edit `terraform.tfvars` with your values:
- **Required**: `smtp_user`, `smtp_password`
- **Optional**: `llm_api_key` (for OpenAI features)
- **Network**: Adjust `allowed_cidr_blocks` to restrict access

### 2. Initialize Terraform

```bash
terraform init
```

### 3. Plan Infrastructure

Review what will be created:

```bash
terraform plan
```

### 4. Deploy Infrastructure

```bash
terraform apply
```

Type `yes` when prompted. This will take 10-15 minutes to create all resources.

### 5. Build and Push Docker Image

After infrastructure is created, get the ECR repository URL from outputs:

```bash
export ECR_REPO=$(terraform output -raw ecr_repository_url)
export AWS_REGION=$(terraform output -raw aws_region)
export AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
```

Build and push the Docker image:

```bash
# Login to ECR
aws ecr get-login-password --region $AWS_REGION | docker login --username AWS --password-stdin $ECR_REPO

# Build Docker image (from project root)
cd ..
docker build -t jf-app:latest -f Dockerfile .

# Tag and push
docker tag jf-app:latest $ECR_REPO:latest
docker push $ECR_REPO:latest
```

### 6. Deploy Application

Force ECS to pull the new image:

```bash
cd tf
aws ecs update-service \
  --cluster $(terraform output -raw ecs_cluster_name) \
  --service $(terraform output -raw ecs_service_name) \
  --force-new-deployment \
  --region $AWS_REGION
```

### 7. Access Application

Get the application URL:

```bash
terraform output alb_url
```

Visit the URL in your browser. It may take a few minutes for the service to become healthy.

## Outputs

After deployment, important information is available as outputs:

```bash
terraform output                          # Show all outputs
terraform output alb_dns_name            # Load balancer DNS
terraform output ecr_repository_url      # Docker image repository
terraform output cloudwatch_log_group    # Logs location
terraform output deployment_instructions # Detailed deployment steps
```

## Configuration

### Environment Variables

The application receives configuration through:
- Environment variables in the ECS task definition
- Secrets from AWS Secrets Manager (SMTP credentials, API keys)
- EFS-mounted persistent storage at `/srv/data`

### Scaling

Auto-scaling is configured for:
- **CPU**: Scales when average CPU > 70%
- **Memory**: Scales when average memory > 70%
- **Range**: 2-10 tasks (configurable via `desired_count` variable)

Manual scaling:

```bash
aws ecs update-service \
  --cluster $(terraform output -raw ecs_cluster_name) \
  --service $(terraform output -raw ecs_service_name) \
  --desired-count 5
```

### HTTPS Setup (Optional)

To enable HTTPS:

1. Request an ACM certificate in AWS Certificate Manager
2. Set variables in `terraform.tfvars`:
   ```hcl
   enable_https    = true
   certificate_arn = "arn:aws:acm:region:account:certificate/xxx"
   ```
3. Apply changes: `terraform apply`

### Custom Domain (Optional)

To use a custom domain:

1. Create a Route 53 hosted zone or use external DNS
2. Add a CNAME record pointing to the ALB DNS name:
   ```
   app.yourdomain.com CNAME jf-app-dev-alb-123456789.us-east-1.elb.amazonaws.com
   ```

## Monitoring

### CloudWatch Logs

View application logs:

```bash
aws logs tail $(terraform output -raw cloudwatch_log_group) --follow
```

### CloudWatch Dashboard

A dashboard is automatically created showing:
- ECS CPU and Memory utilization
- ALB request count and response times
- HTTP 4xx/5xx error counts

Access it in AWS Console → CloudWatch → Dashboards

### Alarms

Two CloudWatch alarms are configured:
- **CPU High**: Triggers when CPU > 80% for 10 minutes
- **Memory High**: Triggers when memory > 80% for 10 minutes

Configure SNS notifications by adding:

```hcl
resource "aws_sns_topic" "alerts" {
  name = "${var.project_name}-${var.environment}-alerts"
}

# Add to alarms:
alarm_actions = [aws_sns_topic.alerts.arn]
```

## Troubleshooting

### Service Won't Start

1. Check ECS service events:
   ```bash
   aws ecs describe-services \
     --cluster $(terraform output -raw ecs_cluster_name) \
     --services $(terraform output -raw ecs_service_name)
   ```

2. Check task logs:
   ```bash
   aws logs tail $(terraform output -raw cloudwatch_log_group) --follow
   ```

### Health Check Failures

- Verify the application starts on port 8080
- Check security groups allow ALB → ECS communication
- Review application logs for errors

### Cannot Push to ECR

```bash
# Re-authenticate
aws ecr get-login-password --region $AWS_REGION | \
  docker login --username AWS --password-stdin $ECR_REPO
```

### EFS Mount Issues

- Verify EFS mount targets are in "available" state
- Check security group allows NFS (port 2049) from ECS tasks
- Review ECS task IAM role has EFS permissions

## Cost Optimization

Estimated monthly costs (us-east-1):
- **ECS Fargate**: ~$30/month (2 tasks, 1vCPU, 2GB)
- **ALB**: ~$16/month
- **NAT Gateways**: ~$65/month (2 AZs)
- **EFS**: ~$0.30/GB/month (depends on data size)
- **CloudWatch**: ~$5/month (logs and metrics)

**Total**: ~$120-150/month

To reduce costs:
1. Use 1 NAT Gateway instead of 2 (reduces HA)
2. Reduce ECS task count to 1 (reduces HA)
3. Use smaller task sizes (512 CPU, 1024 memory)
4. Enable EFS lifecycle policies (automatic IA transition)

## Security Best Practices

✅ **Implemented**:
- Private subnets for ECS tasks
- Security groups with minimal access
- Encrypted EFS and ECR
- IAM roles with least privilege
- Secrets in AWS Secrets Manager
- Container insights enabled
- VPC flow logs ready

🔒 **Additional Recommendations**:
1. Restrict `allowed_cidr_blocks` to your IP range
2. Enable WAF on ALB for DDoS protection
3. Enable GuardDuty for threat detection
4. Use AWS Systems Manager Session Manager instead of SSH
5. Rotate secrets regularly

## Updating the Application

### Code Changes

```bash
# 1. Build new image
docker build -t jf-app:latest .

# 2. Tag and push
docker tag jf-app:latest $ECR_REPO:latest
docker push $ECR_REPO:latest

# 3. Force new deployment
cd tf
aws ecs update-service \
  --cluster $(terraform output -raw ecs_cluster_name) \
  --service $(terraform output -raw ecs_service_name) \
  --force-new-deployment
```

### Infrastructure Changes

```bash
# 1. Update variables in terraform.tfvars or *.tf files
# 2. Plan changes
terraform plan

# 3. Apply changes
terraform apply
```

## Backup and Disaster Recovery

### EFS Backups

Enable AWS Backup for EFS:

```hcl
resource "aws_backup_plan" "efs" {
  name = "${var.project_name}-${var.environment}-efs-backup"

  rule {
    rule_name         = "daily_backup"
    target_vault_name = aws_backup_vault.main.name
    schedule          = "cron(0 2 * * ? *)"  # 2 AM daily

    lifecycle {
      delete_after = 30
    }
  }
}
```

### Database Backups

Since the application uses SQLite on EFS, backups are included in EFS backups.

## Cleanup

To destroy all resources:

```bash
# Warning: This will delete everything!
terraform destroy
```

Type `yes` when prompted. This will:
1. Stop and remove ECS tasks
2. Delete ALB, target groups, listeners
3. Remove EFS file system (data will be lost!)
4. Delete ECR repository (images will be lost!)
5. Remove VPC, subnets, security groups
6. Clean up IAM roles and policies

## Support

For issues with:
- **Infrastructure**: Check Terraform output and AWS Console
- **Application**: Check CloudWatch logs and application README
- **AWS Services**: See AWS documentation or support

## License

This infrastructure code is provided as-is for deploying the JF application.

