resource "crafty_webhook" "alerts" {
  server_id    = crafty_minecraft_server.example.id
  webhook_type = "Discord"
  name         = "Server alerts"
  url          = var.webhook_url
  triggers     = ["start_server", "stop_server", "crash_detected"]
  body         = "{{ server_name }}: {{ event_type }}"
  enabled      = true
}
