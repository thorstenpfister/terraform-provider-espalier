# A development VM with your account and a read-only view of your home
# directory, made the default for `container machine run`.
resource "espalier_machine" "dev" {
  name       = "dev"
  image      = "ubuntu:24.04"
  default    = true
  cpus       = 4
  memory     = "8G"
  home_mount = "ro"
}

output "dev_ip" {
  value = espalier_machine.dev.ip_address
}
