terraform {
  required_version = ">= 1.5.0"
  required_providers {
    crafty = {
      source  = "bart-kochanowicz/crafty"
      version = "0.1.0"
    }
  }
}

variable "crafty_url" {
  type        = string
  description = "Local Docker API bridge URL."
  default     = "http://127.0.0.1:18000"
}

variable "crafty_token" {
  type        = string
  sensitive   = true
  description = "Crafty API bearer token."
}

provider "crafty" {
  url   = var.crafty_url
  token = var.crafty_token
}

resource "crafty_minecraft_server" "example" {
  name    = "Docker smoke test - rename"
  engine  = "paper"
  version = "1.21.1"
  mem_min = 1
  mem_max = 2
  host    = "127.0.0.1"
  port    = 25565

  # These settings can change without replacing the server.
  auto_start      = false
  monitoring_host = "127.0.0.1"
  monitoring_port = 25565

  # Optional full-command override; memory flags take precedence over mem_min/max.
  # execution_command = "java -Xms1500M -Xmx2500M -jar paper.jar nogui"
}

output "server_id" {
  value = crafty_minecraft_server.example.id
}
