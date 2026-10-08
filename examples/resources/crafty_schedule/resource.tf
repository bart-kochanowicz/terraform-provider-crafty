resource "crafty_schedule" "announcement" {
  server_id     = crafty_minecraft_server.example.id
  name          = "Hourly announcement"
  command       = "say Remember to back up your world"
  interval      = 1
  interval_type = "hours"
  enabled       = true
}
