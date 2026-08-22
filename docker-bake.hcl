variable "VERSION" { default = "dev" }
variable "COMMIT" { default = "unknown" }
variable "BUILD_DATE" { default = "unknown" }
variable "REGISTRY" { default = "ghcr.io/j4v3l/skyfeed" }

group "default" {
  targets = ["skyfeed"]
}

target "skyfeed" {
  context = "."
  dockerfile = "Dockerfile"
  platforms = ["linux/amd64", "linux/arm64"]
  tags = ["${REGISTRY}:${VERSION}", "${REGISTRY}:${COMMIT}"]
  args = {
    VERSION = VERSION
    COMMIT = COMMIT
    BUILD_DATE = BUILD_DATE
  }
  attest = ["type=provenance,mode=max", "type=sbom"]
}
