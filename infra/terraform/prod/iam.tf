# Instance role (TASK-aws-infra.md §2.3). The box can write the raw log but never delete or
# lifecycle it: no s3:DeleteObject, s3:DeleteObjectVersion or s3:PutLifecycleConfiguration.

data "aws_iam_policy_document" "ec2_assume" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "app" {
  name               = "transit-pulse-app"
  assume_role_policy = data.aws_iam_policy_document.ec2_assume.json
}

resource "aws_iam_role_policy_attachment" "managed" {
  for_each = {
    ssm        = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
    ecr_read   = "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly"
    cloudwatch = "arn:aws:iam::aws:policy/CloudWatchAgentServerPolicy"
  }

  role       = aws_iam_role.app.name
  policy_arn = each.value
}

data "aws_iam_policy_document" "app_inline" {
  statement {
    sid       = "ReadOwnSecrets"
    actions   = ["ssm:GetParametersByPath", "ssm:GetParameters", "ssm:GetParameter"]
    resources = ["arn:aws:ssm:${var.region}:${data.aws_caller_identity.current.account_id}:parameter${local.ssm_prefix}/*", "arn:aws:ssm:${var.region}:${data.aws_caller_identity.current.account_id}:parameter${local.ssm_prefix}"]
  }

  statement {
    sid       = "ListRawAndOps"
    actions   = ["s3:ListBucket"]
    resources = [aws_s3_bucket.raw.arn, aws_s3_bucket.ops.arn]
  }

  statement {
    sid       = "WriteRaw"
    actions   = ["s3:GetObject", "s3:PutObject"]
    resources = ["${aws_s3_bucket.raw.arn}/*"]
  }

  statement {
    sid       = "ReadDeployFiles"
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.ops.arn}/*"]
  }

  statement {
    sid       = "CustomMetrics"
    actions   = ["cloudwatch:PutMetricData"]
    resources = ["*"]

    condition {
      test     = "StringEquals"
      variable = "cloudwatch:namespace"
      values   = ["TransitPulse"]
    }
  }
}

resource "aws_iam_role_policy" "app_inline" {
  name   = "transit-pulse-app"
  role   = aws_iam_role.app.id
  policy = data.aws_iam_policy_document.app_inline.json
}

resource "aws_iam_instance_profile" "app" {
  name = "transit-pulse-app"
  role = aws_iam_role.app.name
}
