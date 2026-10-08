resource "crafty_backup_config" "daily" {
  server_id     = crafty_minecraft_server.example.id
  backup_id     = var.existing_backup_id
  max_backups   = 7
  compress      = true
  shutdown      = false
  excluded_dirs = ["logs", "cache"]
}
