# A NAT network whose subnet the runtime chooses (it may change when the
# runtime restarts).
resource "espalier_network" "default" {
  name = "apps"
}

# A host-only network with a fixed subnet.
resource "espalier_network" "internal" {
  name     = "backend"
  internal = true
  subnet   = "192.168.100.0/24"
  labels = {
    owner = "platform"
  }
}
