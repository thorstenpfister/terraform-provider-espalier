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
