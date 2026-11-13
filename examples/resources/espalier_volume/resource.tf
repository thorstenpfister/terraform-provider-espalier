# Without a size, the runtime creates a sparse 512 GiB disk image.
resource "espalier_volume" "data" {
  name = "registry-data"
}

resource "espalier_volume" "cache" {
  name = "build-cache"
  size = "20G"
  labels = {
    owner = "ci"
  }
}
