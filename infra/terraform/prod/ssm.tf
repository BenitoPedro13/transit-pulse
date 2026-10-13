# Secret *names* only (TASK-aws-infra.md §2.3). Terraform creates each parameter with a
# placeholder and ignores its value; the real values are set once from the Mac with
# `aws ssm put-parameter --overwrite`, so secret values never land in Terraform state.

locals {
  ssm_prefix = "/transit-pulse/prod"

  secret_names = toset([
    "POSTGRES_PASSWORD",
    "ITS_CONTACT",
  ])
}

resource "aws_ssm_parameter" "secret" {
  for_each = local.secret_names

  name  = "${local.ssm_prefix}/${each.key}"
  type  = "SecureString"
  value = "UNSET"

  lifecycle {
    ignore_changes = [value]
  }
}
