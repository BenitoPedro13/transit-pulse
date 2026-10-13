# Creates the one resource every other Terraform config here depends on: the S3 bucket that
# holds remote state (docs/tasks/TASK-aws-infra.md §2.3). Applied once with local state; then
# backend.tf (the `backend "s3"` block) is added and `terraform init -migrate-state` moves this
# config's own state into the bucket it created, so no state file lives on a laptop.

terraform {
  required_version = ">= 1.11"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.66"
    }
  }
}

provider "aws" {
  region = "us-east-1"

  default_tags {
    tags = {
      project   = "transit-pulse"
      ManagedBy = "terraform"
      Stack     = "bootstrap"
    }
  }
}

data "aws_caller_identity" "current" {}

resource "aws_s3_bucket" "tfstate" {
  # Account ID suffix keeps the globally-unique name deterministic without a random suffix.
  bucket = "transit-pulse-tfstate-${data.aws_caller_identity.current.account_id}"

  # Losing state means Terraform forgets everything it manages. Removing this guard is a
  # deliberate step of the exit runbook (TASK-aws-infra.md §2.3), not something a stray
  # `destroy` should be able to do.
  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_s3_bucket_versioning" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

# Versioning keeps every past state; expire the old ones so the bucket doesn't grow forever.
resource "aws_s3_bucket_lifecycle_configuration" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id

  rule {
    id     = "expire-noncurrent-state"
    status = "Enabled"

    filter {}

    noncurrent_version_expiration {
      noncurrent_days           = 90
      newer_noncurrent_versions = 10
    }
  }
}

output "state_bucket" {
  value = aws_s3_bucket.tfstate.bucket
}
