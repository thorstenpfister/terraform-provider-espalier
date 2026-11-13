resource "espalier_image" "nginx" {
  name = "nginx:1.29-alpine"
}

resource "espalier_network" "apps" {
  name   = "apps"
  subnet = "192.168.100.0/24"
}

resource "espalier_volume" "cache" {
  name = "nginx-cache"
  size = "1G"
}

resource "espalier_container" "web" {
  name   = "web"
  image  = espalier_image.nginx.name
  cpus   = 1
  memory = "256M"
  env    = { NGINX_ENTRYPOINT_QUIET_LOGS = "1" }
  labels = { owner = "terraform" }

  networks = [
    { network = espalier_network.apps.name, aliases = ["web.apps.test"] },
  ]
  mounts = [
    { type = "bind", source = "/Users/me/site", target = "/usr/share/nginx/html", read_only = true },
    { type = "volume", source = espalier_volume.cache.name, target = "/var/cache/nginx" },
    { type = "tmpfs", target = "/tmp" },
  ]
  ports = [
    { host_ip = "127.0.0.1", host_port = 8080, container_port = 80 },
  ]
  cap_drop = ["CAP_NET_RAW"]
}

output "web_ipv4" {
  value = espalier_container.web.addresses[0].ipv4_address
}

# A locked-down worker: x86-64 image under Rosetta, unprivileged user,
# read-only root, own resolver, and an image from a local registry
# without TLS.
resource "espalier_container" "worker" {
  name      = "worker"
  image     = "localhost:5000/worker:1.0"
  scheme    = "http"
  platform  = "linux/amd64"
  rosetta   = true
  user      = "1000:1000"
  read_only = true
  init      = true
  shm_size  = "128M"
  cap_drop  = ["ALL"]

  dns = {
    nameservers    = ["192.168.100.53"]
    search_domains = ["apps.test"]
  }
  ulimits = [{ name = "nofile", soft = 4096, hard = 8192 }]
  mounts  = [{ type = "tmpfs", target = "/tmp" }]
}
