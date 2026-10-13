# Added after the first apply created this bucket; `terraform init -migrate-state` then moved
# the bootstrap's own state here (see main.tf header).
terraform {
  backend "s3" {
    bucket       = "transit-pulse-tfstate-860897618882"
    key          = "bootstrap/terraform.tfstate"
    region       = "us-east-1"
    encrypt      = true
    use_lockfile = true
  }
}
