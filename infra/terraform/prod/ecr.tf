resource "aws_ecr_repository" "go" {
  name = "transit-pulse/go"

  # Tags are git SHAs; an immutable tag always means the same image.
  image_tag_mutability = "IMMUTABLE"

  # Lets the exit runbook's `terraform destroy` remove a repo that still holds images.
  force_delete = true

  image_scanning_configuration {
    scan_on_push = true
  }
}

resource "aws_ecr_lifecycle_policy" "go" {
  repository = aws_ecr_repository.go.name

  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Expire untagged images after 1 day"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = 1
        }
        action = { type = "expire" }
      },
      {
        rulePriority = 2
        description  = "Keep the last 5 tagged images"
        selection = {
          tagStatus      = "tagged"
          tagPatternList = ["*"]
          countType      = "imageCountMoreThan"
          countNumber    = 5
        }
        action = { type = "expire" }
      },
    ]
  })
}
