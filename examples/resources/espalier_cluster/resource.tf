resource "espalier_cluster" "dev" {
  name    = "dev"
  cpus    = 4
  memory  = "4G"
  workers = [{ count = 2, cpus = 2, memory = "4G" }]
  cni     = "NONE" # install Cilium yourself

  timeouts {
    create = "20m"
  }
}

# The provider never touches ~/.kube/config: write a file of your own...
resource "local_sensitive_file" "kubeconfig" {
  filename        = "${path.module}/kubeconfig"
  content         = espalier_cluster.dev.kubeconfig
  file_permission = "0600"
}

# ...or configure other providers directly.
provider "kubernetes" {
  host                   = espalier_cluster.dev.api_server
  client_certificate     = espalier_cluster.dev.client_certificate
  client_key             = espalier_cluster.dev.client_key
  cluster_ca_certificate = espalier_cluster.dev.cluster_ca_certificate
}
