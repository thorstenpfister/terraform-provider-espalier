resource "espalier_image" "nginx" {
  name = "nginx:1.29-alpine"
}

# Containers refer to "web:current"; moving the tag swaps the image they
# are created from.
resource "espalier_image_tag" "web" {
  source = espalier_image.nginx.name
  target = "web:current"
}
