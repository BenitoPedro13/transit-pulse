# One public subnet, no NAT gateway (TASK-aws-infra.md §2.0): the instance reaches the internet
# through the IGW with its Elastic IP, and the security group is the only thing exposed.

resource "aws_vpc" "main" {
  cidr_block           = "10.30.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "transit-pulse" }
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id

  tags = { Name = "transit-pulse" }
}

resource "aws_subnet" "public" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.30.1.0/24"
  availability_zone = var.availability_zone

  # Public IPs are requested explicitly on the instance, not handed to anything in the subnet.
  map_public_ip_on_launch = false

  tags = { Name = "transit-pulse-public-${var.availability_zone}" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }

  tags = { Name = "transit-pulse-public" }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

# Strip every rule from the VPC's default security group so nothing can accidentally use it
# (CIS AWS Foundations benchmark control).
resource "aws_default_security_group" "default" {
  vpc_id = aws_vpc.main.id
}

resource "aws_security_group" "app" {
  name        = "transit-pulse-app"
  description = "Caddy HTTP/HTTPS only. No SSH: shell access is SSM Session Manager."
  vpc_id      = aws_vpc.main.id

  tags = { Name = "transit-pulse-app" }
}

# 80 stays open for Let's Encrypt's HTTP-01 challenge and Caddy's HTTP->HTTPS redirect.
resource "aws_vpc_security_group_ingress_rule" "http" {
  security_group_id = aws_security_group.app.id
  description       = "HTTP (ACME challenge + redirect)"
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "tcp"
  from_port         = 80
  to_port           = 80
}

resource "aws_vpc_security_group_ingress_rule" "https" {
  security_group_id = aws_security_group.app.id
  description       = "HTTPS (API via Caddy)"
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
}

resource "aws_vpc_security_group_egress_rule" "all" {
  security_group_id = aws_security_group.app.id
  description       = "Outbound: provider APIs, ECR, SSM, package repos"
  cidr_ipv4         = "0.0.0.0/0"
  ip_protocol       = "-1"
}
