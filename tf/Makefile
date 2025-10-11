.PHONY: help init plan apply destroy validate fmt clean login push deploy

# Variables
PROJECT_NAME ?= jf-app
AWS_REGION ?= us-east-1

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

init: ## Initialize Terraform
	terraform init

validate: ## Validate Terraform configuration
	terraform validate

fmt: ## Format Terraform files
	terraform fmt -recursive

plan: ## Run Terraform plan
	terraform plan

apply: ## Apply Terraform changes
	terraform apply

destroy: ## Destroy all Terraform resources
	@echo "WARNING: This will destroy all resources!"
	@read -p "Are you sure? Type 'yes' to continue: " confirm && [ "$$confirm" = "yes" ]
	terraform destroy

clean: ## Clean Terraform cache
	rm -rf .terraform
	rm -f .terraform.lock.hcl
	rm -f terraform.tfstate*

login: ## Login to ECR
	@aws ecr get-login-password --region $(AWS_REGION) | docker login --username AWS --password-stdin $$(terraform output -raw ecr_repository_url 2>/dev/null || echo "")

build: ## Build Docker image
	cd .. && docker build -t $(PROJECT_NAME):latest -f Dockerfile .

push: login build ## Build and push Docker image to ECR
	@ECR_REPO=$$(terraform output -raw ecr_repository_url); \
	docker tag $(PROJECT_NAME):latest $$ECR_REPO:latest; \
	docker push $$ECR_REPO:latest

deploy: push ## Deploy new version to ECS
	@CLUSTER=$$(terraform output -raw ecs_cluster_name); \
	SERVICE=$$(terraform output -raw ecs_service_name); \
	aws ecs update-service --cluster $$CLUSTER --service $$SERVICE --force-new-deployment --region $(AWS_REGION)

logs: ## Tail CloudWatch logs
	@LOG_GROUP=$$(terraform output -raw cloudwatch_log_group); \
	aws logs tail $$LOG_GROUP --follow --region $(AWS_REGION)

status: ## Show ECS service status
	@CLUSTER=$$(terraform output -raw ecs_cluster_name); \
	SERVICE=$$(terraform output -raw ecs_service_name); \
	aws ecs describe-services --cluster $$CLUSTER --services $$SERVICE --region $(AWS_REGION) --query 'services[0].{Status:status,Running:runningCount,Desired:desiredCount,Pending:pendingCount}' --output table

url: ## Show application URL
	@terraform output alb_url

outputs: ## Show all Terraform outputs
	@terraform output

cost: ## Estimate monthly costs (requires infracost)
	@command -v infracost >/dev/null 2>&1 || { echo "infracost not installed. Visit: https://www.infracost.io/docs/"; exit 1; }
	infracost breakdown --path .

security: ## Run security scan (requires tfsec)
	@command -v tfsec >/dev/null 2>&1 || { echo "tfsec not installed. Visit: https://github.com/aquasecurity/tfsec"; exit 1; }
	tfsec .

all: init validate fmt plan ## Initialize, validate, format, and plan

.DEFAULT_GOAL := help

