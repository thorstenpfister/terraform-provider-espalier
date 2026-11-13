terraform {
  required_providers {
    espalier = {
      source = "thorstenpfister/espalier"
    }
  }
}

provider "espalier" {
  # Optional: size the builder container that runs espalier_image_build.
  # It is stopped after the builds unless keep_running = true.
  builder = {
    cpus   = 4
    memory = "4G"
  }
}
