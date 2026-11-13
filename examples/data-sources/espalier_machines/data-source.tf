data "espalier_machines" "all" {}

output "default_machine" {
  value = one([for m in data.espalier_machines.all.machines : m.name if m.default])
}
