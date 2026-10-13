variable "region" {
  type    = string
  default = "us-east-1"
}

# Pinned: the data volume is AZ-bound. 1c had the lowest t4g.small Spot price on 2026-10-13
# (TASK-aws-infra.md §6, Phase 0; chosen by the user).
variable "availability_zone" {
  type    = string
  default = "us-east-1c"
}

variable "instance_type" {
  type    = string
  default = "t4g.small"

  # Free-plan eligible Graviton types only. m7i-flex.large (x86) is the documented fallback and
  # would need a linux/amd64 image, so it is a deliberate edit, not a variable value.
  validation {
    condition     = contains(["t4g.micro", "t4g.small"], var.instance_type)
    error_message = "Use a free-tier-eligible Graviton type (t4g.micro or t4g.small)."
  }
}

variable "use_spot" {
  type    = bool
  default = true
}

# 12 GiB sufficed for renewable-pulse; +4 for the local raw buffer and tooling (§2.3).
variable "root_volume_gb" {
  type    = number
  default = 16
}

# positions + redpanda + raw log buffer (architecture §8). Grows online if the disk alarm fires.
variable "data_volume_gb" {
  type    = number
  default = 30
}

variable "compose_version" {
  type    = string
  default = "v5.5.1"
}

# sha256 of docker-compose-linux-aarch64 for compose_version (same release as renewable-pulse).
variable "compose_sha256" {
  type    = string
  default = "732e3a84c1a0f67256ce80bc2598a24546b10ca05f9faa97efceb1171ece2ef7"
}

# Origins allowed to read the tiles bucket through CloudFront (CORS). "*" until the Vercel
# production URL exists (Phase 4); tiles are public map data, so this is tightened, not secret.
variable "tiles_cors_origins" {
  type    = list(string)
  default = ["*"]
}
