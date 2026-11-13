# Volumes labelled by another configuration.
data "espalier_volumes" "backups" {
  labels = { purpose = "backup" }
}

output "backup_volumes" {
  value = data.espalier_volumes.backups.volumes[*].name
}
