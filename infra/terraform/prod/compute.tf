# Latest Amazon Linux 2023 arm64 (SSM agent preinstalled). `insecure_value` because this public
# parameter is a plain String, not a secret, and `value` is always marked sensitive.
data "aws_ssm_parameter" "al2023_arm64" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-arm64"
}

# Separate from the instance on purpose: replacing or rebuilding the instance never touches the
# database (TASK-aws-infra.md §2.3). Created before the instance so user-data can find it by ID.
resource "aws_ebs_volume" "data" {
  availability_zone = var.availability_zone
  size              = var.data_volume_gb
  type              = "gp3"
  encrypted         = true

  tags = {
    Name   = "transit-pulse-data"
    Role = "data"
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_instance" "app" {
  ami                  = data.aws_ssm_parameter.al2023_arm64.insecure_value
  instance_type        = var.instance_type
  subnet_id            = aws_subnet.public.id
  iam_instance_profile = aws_iam_instance_profile.app.name

  vpc_security_group_ids = [aws_security_group.app.id]

  # Needed for internet access during first boot (dnf, Docker Compose download) before the
  # Elastic IP is associated. Associating the EIP releases this auto-assigned address, so only
  # one public IPv4 is billed at a time.
  associate_public_ip_address = true

  user_data = templatefile("${path.module}/user-data.sh.tftpl", {
    compose_version = var.compose_version
    compose_sha256  = var.compose_sha256
    # NVMe by-id links drop the dash: vol-0abc -> nvme-Amazon_Elastic_Block_Store_vol0abc
    data_volume_device = "/dev/disk/by-id/nvme-Amazon_Elastic_Block_Store_${replace(aws_ebs_volume.data.id, "-", "")}"
  })

  metadata_options {
    http_endpoint = "enabled"
    http_tokens   = "required" # IMDSv2 only
    # 1 = only the host itself can reach instance metadata, not containers behind Docker's NAT.
    # Nothing in a container needs the instance role; ECR login and SSM run on the host.
    http_put_response_hop_limit = 1
  }

  root_block_device {
    volume_type           = "gp3"
    volume_size           = var.root_volume_gb
    encrypted             = true
    delete_on_termination = true
  }

  # Persistent Spot request that *stops* on interruption instead of terminating. The instance
  # and its volumes are kept, and AWS starts it again when capacity returns.
  dynamic "instance_market_options" {
    for_each = var.use_spot ? [1] : []

    content {
      market_type = "spot"

      spot_options {
        spot_instance_type             = "persistent"
        instance_interruption_behavior = "stop"
      }
    }
  }

  lifecycle {
    # A new AL2023 release or a user-data edit must never silently replace the running box.
    # After first boot, host changes go through SSM (deploy.sh), not user-data.
    ignore_changes = [ami, user_data]
  }

  tags = { Name = "transit-pulse-app" }
}

resource "aws_volume_attachment" "data" {
  device_name = "/dev/sdf"
  volume_id   = aws_ebs_volume.data.id
  instance_id = aws_instance.app.id

  # Stop the instance before detaching, so Postgres is never cut off mid-write.
  stop_instance_before_detaching = true
}

# EIP and its association are separate resources, so the address survives an instance
# replacement and the API hostname (§2.4) never changes.
resource "aws_eip" "app" {
  domain = "vpc"

  tags = { Name = "transit-pulse-app" }
}

resource "aws_eip_association" "app" {
  allocation_id = aws_eip.app.id
  instance_id   = aws_instance.app.id
}
