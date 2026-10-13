output "instance_id" {
  value = aws_instance.app.id
}

output "public_ip" {
  value = aws_eip.app.public_ip
}

output "ecr_repository_url" {
  value = aws_ecr_repository.go.repository_url
}

output "raw_bucket" {
  value = aws_s3_bucket.raw.bucket
}

output "ops_bucket" {
  value = aws_s3_bucket.ops.bucket
}

output "tiles_bucket" {
  value = aws_s3_bucket.tiles.bucket
}

output "tiles_base_url" {
  description = "Value for NEXT_PUBLIC_TILES_BASE_URL"
  value       = "https://${aws_s3_bucket.tiles.bucket_regional_domain_name}"
}

output "ssm_session_command" {
  description = "Open a shell on the instance (needs the session-manager-plugin installed)"
  value       = "aws ssm start-session --target ${aws_instance.app.id}"
}
