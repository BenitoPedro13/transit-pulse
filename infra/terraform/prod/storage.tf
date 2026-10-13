# Three buckets (TASK-aws-infra.md §2.3). raw and ops are private. tiles is public-read: CloudFront
# was refused ("account must be verified", 2026-10-13), so the map's tiles, glyphs and sprites are
# served straight from S3 (it honours Range natively). They are public map data, nothing else lives there.

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
  for_each = { raw = aws_s3_bucket.raw.id, ops = aws_s3_bucket.ops.id }

  bucket                  = each.value
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "all" {
  for_each = { raw = aws_s3_bucket.raw.id, ops = aws_s3_bucket.ops.id }

  bucket = each.value

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "all" {
  for_each = { raw = aws_s3_bucket.raw.id, ops = aws_s3_bucket.ops.id }

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

# --- Tiles: public-read bucket, CORS for pmtiles' Range requests ------------------------------

resource "aws_s3_bucket_public_access_block" "tiles" {
  bucket = aws_s3_bucket.tiles.id

  block_public_acls       = true
  ignore_public_acls      = true
  block_public_policy     = false
  restrict_public_buckets = false
}

resource "aws_s3_bucket_ownership_controls" "tiles" {
  bucket = aws_s3_bucket.tiles.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "tiles" {
  bucket = aws_s3_bucket.tiles.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

data "aws_iam_policy_document" "tiles_public" {
  statement {
    sid       = "PublicRead"
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.tiles.arn}/*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }
  }
}

resource "aws_s3_bucket_policy" "tiles" {
  bucket = aws_s3_bucket.tiles.id
  policy = data.aws_iam_policy_document.tiles_public.json

  depends_on = [aws_s3_bucket_public_access_block.tiles]
}

resource "aws_s3_bucket_cors_configuration" "tiles" {
  bucket = aws_s3_bucket.tiles.id

  cors_rule {
    allowed_methods = ["GET", "HEAD"]
    allowed_origins = var.tiles_cors_origins
    allowed_headers = ["*"]
    expose_headers  = ["Content-Range", "Content-Length", "ETag"]
    max_age_seconds = 86400
  }
}
