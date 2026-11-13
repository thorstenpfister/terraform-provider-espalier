data "espalier_containers" "lab" {
  labels = { owner = "terraform" }
}

output "running" {
  value = [for c in data.espalier_containers.lab.containers : c.name if c.running]
}
