# An image built with `container build -t app:dev .`
data "espalier_image" "app" {
  name = "app:dev"
}

output "app_digest" {
  value = data.espalier_image.app.digest
}
