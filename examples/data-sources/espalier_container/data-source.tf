# A container started outside Terraform, e.g. with `container run`.
data "espalier_container" "dns" {
  name = "lab-dns"
}

output "dns_ipv4" {
  value = data.espalier_container.dns.addresses[0].ipv4_address
}
