terraform {
  required_providers {
    crafty = {
      source  = "bart-kochanowicz/crafty"
      version = "~> 0.1.2"
    }
  }
}

variable "crafty_url" {
  type        = string
  description = "Crafty panel base URL."
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
  name    = "Terraform Minecraft"
  engine  = "paper"
  version = "1.21.1"
  mem_min = 1
  mem_max = 2
  host    = "127.0.0.1"
  port    = 25565
}

output "server_id" {
  value = crafty_minecraft_server.example.id
}
