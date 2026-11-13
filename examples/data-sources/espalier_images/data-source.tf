data "espalier_images" "all" {}

output "image_names" {
  value = data.espalier_images.all.images[*].name
}
