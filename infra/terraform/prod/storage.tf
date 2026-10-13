# Three buckets (TASK-aws-infra.md §2.3). All private, SSE-S3, public access blocked.

locals {
  account_id = data.aws_caller_identity.current.account_id
  buckets = {
    raw   = "transit-pulse-raw-${local.account_id}"
    ops   = "transit-pulse-ops-${local.account_id}"
    tiles = "transit-pulse-tiles-${local.account_id}"
  }
}

resource "aws_s3_bucket" "raw" {
  bucket = local.buckets.raw

  # The raw log is invariant 2. A stray `destroy` must not remove it; the exit runbook
  # (architecture §7.1) removes this guard deliberately, after pull-raw.sh has verified a copy.
  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_s3_bucket" "ops" {
  bucket = local.buckets.ops
}

resource "aws_s3_bucket" "tiles" {
  bucket = local.buckets.tiles
}

resource "aws_s3_bucket_public_access_block" "all" {
  for_each = { raw = aws_s3_bucket.raw.id, ops = aws_s3_bucket.ops.id, tiles = aws_s3_bucket.tiles.id }

  bucket                  = each.value
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "all" {
  for_each = { raw = aws_s3_bucket.raw.id, ops = aws_s3_bucket.ops.id, tiles = aws_s3_bucket.tiles.id }

  bucket = each.value

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "all" {
  for_each = { raw = aws_s3_bucket.raw.id, ops = aws_s3_bucket.ops.id, tiles = aws_s3_bucket.tiles.id }

  bucket = each.value

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# A re-upload of a growing hour file keeps the previous version: nothing is lost to an overwrite.
resource "aws_s3_bucket_versioning" "raw" {
  bucket = aws_s3_bucket.raw.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "raw" {
  bucket = aws_s3_bucket.raw.id

  # Versioning must be on before the noncurrent-version rule is accepted.
  depends_on = [aws_s3_bucket_versioning.raw]

  rule {
    id     = "current-to-ia-after-30d"
    status = "Enabled"

    filter {}

    transition {
      days          = 30
      storage_class = "STANDARD_IA"
    }
  }

  # Safe: raw files are append-only, so an older version is always a prefix of the newer one.
  rule {
    id     = "expire-noncurrent-after-30d"
    status = "Enabled"

    filter {}

    noncurrent_version_expiration {
      noncurrent_days = 30
    }

    abort_incomplete_multipart_upload {
      days_after_initiation = 7
    }
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "ops" {
  bucket = aws_s3_bucket.ops.id

  rule {
    id     = "expire-deploy-files"
    status = "Enabled"

    filter {
      prefix = "deploy/"
    }

    expiration {
      days = 30
    }
  }
}

# --- Tiles: private bucket behind CloudFront with Origin Access Control -----------------------

resource "aws_cloudfront_origin_access_control" "tiles" {
  name                              = "transit-pulse-tiles"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

# CORS for pmtiles' Range requests. Expose Content-Range/Content-Length/ETag so the client can
# read the range response.
resource "aws_cloudfront_response_headers_policy" "tiles" {
  name = "transit-pulse-tiles-cors"

  cors_config {
    access_control_allow_credentials = false
    origin_override                  = true

    access_control_allow_headers {
      items = ["*"]
    }

    access_control_allow_methods {
      items = ["GET", "HEAD", "OPTIONS"]
    }

    access_control_allow_origins {
      items = var.tiles_cors_origins
    }

    access_control_expose_headers {
      items = ["Content-Range", "Content-Length", "ETag"]
    }

    access_control_max_age_sec = 86400
  }
}

# AWS-managed "CachingOptimized" cache policy.
data "aws_cloudfront_cache_policy" "optimized" {
  name = "Managed-CachingOptimized"
}

resource "aws_cloudfront_distribution" "tiles" {
  enabled         = true
  comment         = "transit-pulse tiles, glyphs and sprites"
  price_class     = "PriceClass_100"
  http_version    = "http2and3"
  is_ipv6_enabled = true

  origin {
    domain_name              = aws_s3_bucket.tiles.bucket_regional_domain_name
    origin_id                = "tiles"
    origin_access_control_id = aws_cloudfront_origin_access_control.tiles.id
  }

  default_cache_behavior {
    target_origin_id           = "tiles"
    viewer_protocol_policy     = "redirect-to-https"
    allowed_methods            = ["GET", "HEAD", "OPTIONS"]
    cached_methods             = ["GET", "HEAD"]
    compress                   = true
    cache_policy_id            = data.aws_cloudfront_cache_policy.optimized.id
    response_headers_policy_id = aws_cloudfront_response_headers_policy.tiles.id
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }
}

data "aws_iam_policy_document" "tiles_bucket" {
  statement {
    sid       = "CloudFrontRead"
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.tiles.arn}/*"]

    principals {
      type        = "Service"
      identifiers = ["cloudfront.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "AWS:SourceArn"
      values   = [aws_cloudfront_distribution.tiles.arn]
    }
  }
}

resource "aws_s3_bucket_policy" "tiles" {
  bucket = aws_s3_bucket.tiles.id
  policy = data.aws_iam_policy_document.tiles_bucket.json
}
