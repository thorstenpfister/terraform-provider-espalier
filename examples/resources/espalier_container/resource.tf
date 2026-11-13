resource "espalier_image" "busybox" {
  name = "busybox:1.37"
}

resource "espalier_container" "sleeper" {
  name    = "sleeper"
  image   = espalier_image.busybox.name
  command = ["sleep", "infinity"]
  env     = { TZ = "Europe/Berlin" }
  cpus    = 1
  memory  = "256M"
  labels  = { owner = "terraform" }
}

output "sleeper_ipv4" {
  value = espalier_container.sleeper.addresses[0].ipv4_address
}
