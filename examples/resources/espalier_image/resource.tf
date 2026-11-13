resource "espalier_image" "busybox" {
  name     = "busybox:1.37"
  platform = "linux/arm64"
}

# Pinned by digest, and kept when this configuration is destroyed.
resource "espalier_image" "registry" {
  name         = "ghcr.io/project-zot/zot@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  keep_locally = true
}
