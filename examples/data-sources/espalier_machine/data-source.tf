data "espalier_machine" "dev" {
  name = "dev"
}

output "dev_ip" {
  value = data.espalier_machine.dev.ip_address
}
