# Fail early, with a useful message, when the runtime is not started.
data "espalier_system" "this" {
  lifecycle {
    postcondition {
      condition     = self.running
      error_message = "The container runtime is ${self.status}: run `container system start`."
    }
  }
}

output "runtime" {
  value = "container ${data.espalier_system.this.client_version}, kernel ${data.espalier_system.this.kernel_binary_path}"
}
