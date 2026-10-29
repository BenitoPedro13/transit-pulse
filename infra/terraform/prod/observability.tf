# Alarms (docs/tasks/TASK-aws-infra.md §2.6). Every alarm notifies the SNS topic, which emails
# var.alert_email once the subscription is confirmed (a link in the first email).

variable "alert_email" {
  type        = string
  description = "Where alarm emails go. Set it in terraform.tfvars (git-ignored); do not commit it."
}

resource "aws_sns_topic" "alarms" {
  name = "transit-pulse-alarms"
}

resource "aws_sns_topic_subscription" "email" {
  topic_arn = aws_sns_topic.alarms.arn
  protocol  = "email"
  endpoint  = var.alert_email
}

locals {
  alarm_actions = [aws_sns_topic.alarms.arn]
}

# architecture §8 (renewable-pulse abb09de): a full disk stops the database.
resource "aws_cloudwatch_metric_alarm" "disk" {
  alarm_name          = "transit-pulse-data-disk"
  alarm_description   = "/data is at least 70% full"
  namespace           = "CWAgent"
  metric_name         = "disk_used_percent"
  dimensions          = { InstanceId = aws_instance.app.id }
  statistic           = "Maximum"
  period              = 300
  evaluation_periods  = 3
  threshold           = 70
  comparison_operator = "GreaterThanOrEqualToThreshold"
  treat_missing_data  = "missing"
  alarm_actions       = local.alarm_actions
}

resource "aws_cloudwatch_metric_alarm" "memory" {
  alarm_name          = "transit-pulse-memory"
  alarm_description   = "Host memory at least 90% for 10 minutes"
  namespace           = "CWAgent"
  metric_name         = "mem_used_percent"
  dimensions          = { InstanceId = aws_instance.app.id }
  statistic           = "Average"
  period              = 300
  evaluation_periods  = 2
  threshold           = 90
  comparison_operator = "GreaterThanOrEqualToThreshold"
  treat_missing_data  = "missing"
  alarm_actions       = local.alarm_actions
}

resource "aws_cloudwatch_metric_alarm" "status_check" {
  alarm_name          = "transit-pulse-status-check"
  alarm_description   = "EC2 status check failed"
  namespace           = "AWS/EC2"
  metric_name         = "StatusCheckFailed"
  dimensions          = { InstanceId = aws_instance.app.id }
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 2
  threshold           = 1
  comparison_operator = "GreaterThanOrEqualToThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = local.alarm_actions
}

# Invariant 2: the raw log is not reaching S3. The timer runs every 10 minutes; two empty hours.
resource "aws_cloudwatch_metric_alarm" "raw_sync" {
  alarm_name          = "transit-pulse-raw-sync"
  alarm_description   = "No successful raw-sync for 2 hours"
  namespace           = "TransitPulse"
  metric_name         = "RawSyncSucceeded"
  statistic           = "Sum"
  period              = 3600
  evaluation_periods  = 2
  threshold           = 1
  comparison_operator = "LessThanThreshold"
  treat_missing_data  = "breaching"
  alarm_actions       = local.alarm_actions
}

# Nothing is being recorded. raw-sync emits this metric every 10 minutes, so the window is two
# 10-minute periods rather than the plan's 10 minutes (a single sparse point can't be evaluated).
resource "aws_cloudwatch_metric_alarm" "adapter_stall" {
  alarm_name          = "transit-pulse-adapter-stall"
  alarm_description   = "The newest raw file is older than 15 minutes"
  namespace           = "TransitPulse"
  metric_name         = "RawNewestFileAgeSeconds"
  statistic           = "Maximum"
  period              = 600
  evaluation_periods  = 2
  threshold           = 900
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "breaching"
  alarm_actions       = local.alarm_actions
}

# A Spot interruption stops the instance (persistent request, stop behaviour): say so.
resource "aws_cloudwatch_event_rule" "stopped" {
  name = "transit-pulse-instance-stopped"
  event_pattern = jsonencode({
    source        = ["aws.ec2"]
    "detail-type" = ["EC2 Instance State-change Notification"]
    detail        = { state = ["stopped"], "instance-id" = [aws_instance.app.id] }
  })
}

resource "aws_cloudwatch_event_target" "stopped" {
  rule = aws_cloudwatch_event_rule.stopped.name
  arn  = aws_sns_topic.alarms.arn
}

data "aws_iam_policy_document" "alarms_topic" {
  statement {
    actions   = ["sns:Publish"]
    resources = [aws_sns_topic.alarms.arn]

    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com", "cloudwatch.amazonaws.com"]
    }
  }
}

resource "aws_sns_topic_policy" "alarms" {
  arn    = aws_sns_topic.alarms.arn
  policy = data.aws_iam_policy_document.alarms_topic.json
}
