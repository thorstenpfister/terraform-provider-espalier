# The runtime's own network, e.g. to read its subnet.
data "espalier_network" "default" {
  name = "default"
}

output "default_subnet" {
  value = data.espalier_network.default.subnet
}
