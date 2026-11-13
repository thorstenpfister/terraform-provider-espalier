resource "espalier_image_build" "app" {
  context    = "${path.module}/app"
  tags       = ["app:dev"]
  build_args = { GO_VERSION = "1.25" }

  # Read from the host when building; never stored in the state.
  secrets = { npm_token = { env = "NPM_TOKEN" } }

  # Rebuilds happen when the context changes; bump this to force one.
  triggers = { rebuild = "1" }
}

resource "espalier_container" "app" {
  name  = "app"
  image = espalier_image_build.app.id

  # A rebuild keeps the tag, so recreate the container on a new digest.
  lifecycle {
    replace_triggered_by = [espalier_image_build.app.digest]
  }
}
