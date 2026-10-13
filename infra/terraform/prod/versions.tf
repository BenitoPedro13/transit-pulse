terraform {
  required_version = ">= 1.11"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.66"
    }
  }

  # Bucket created by ../bootstrap. Native S3 lockfile locking (no DynamoDB table).
  backend "s3" {
    bucket       = "transit-pulse-tfstate-860897618882"
    key          = "prod/terraform.tfstate"
    region       = "us-east-1"
    encrypt      = true
    use_lockfile = true
  }
}

provider "aws" {
  region = var.region

  # `project` is the key the transit-pulse-monthly budget filters on (TASK-aws-infra.md §2.2).
  default_tags {
    tags = {
      project   = "transit-pulse"
      ManagedBy = "terraform"
      Stack     = "prod"
    }
  }
}

data "aws_caller_identity" "current" {}
