data "espalier_networks" "all" {}

output "subnets" {
  value = { for n in data.espalier_networks.all.networks : n.name => n.subnet }
}
