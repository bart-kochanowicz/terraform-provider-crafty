package client

// CreateJavaServerRequest follows Crafty's Java download API contract.
type CreateJavaServerRequest struct {
	Name           string         `json:"name"`
	MonitoringType string         `json:"monitoring_type"`
	CreateType     string         `json:"create_type"`
	Monitoring     JavaMonitoring `json:"minecraft_java_monitoring_data"`
	Java           JavaCreateData `json:"minecraft_java_create_data"`
}

// JavaMonitoring configures Crafty's Minecraft monitoring endpoint.
type JavaMonitoring struct {
	Host string `json:"host"`
	Port int64  `json:"port"`
}

// JavaCreateData selects the Java download operation.
type JavaCreateData struct {
	CreateType string       `json:"create_type"`
	Download   JavaDownload `json:"download_jar_create_data"`
}

// JavaDownload specifies the engine, version, memory, and server port.
// Crafty download_jar multiplies each memory input by 1000 for JVM M flags.
type JavaDownload struct {
	Category string `json:"category"`
	Engine   string `json:"type"`
	Version  string `json:"version"`
	MemMin   int64  `json:"mem_min"`
	MemMax   int64  `json:"mem_max"`
	Port     int64  `json:"server_properties_port"`
}

// CreatedServer contains the identifier returned by server creation.
type CreatedServer struct {
	ID string `json:"new_server_id"`
}

// Server contains the fields required to refresh Terraform state.
// Pointers distinguish missing response fields from valid zero values.
type Server struct {
	ID               string  `json:"server_id"`
	Name             *string `json:"server_name"`
	AutoStart        *bool   `json:"auto_start"`
	MonitoringHost   *string `json:"server_ip"`
	MonitoringPort   *int64  `json:"server_port"`
	ExecutionCommand *string `json:"execution_command"`
}

// UpdateServerRequest contains verified mutable Crafty configuration fields.
// Optional pointers distinguish omitted fields from false and other zero values.
type UpdateServerRequest struct {
	Name             string  `json:"server_name,omitempty"`
	AutoStart        *bool   `json:"auto_start,omitempty"`
	MonitoringHost   *string `json:"server_ip,omitempty"`
	MonitoringPort   *int64  `json:"server_port,omitempty"`
	ExecutionCommand *string `json:"execution_command,omitempty"`
}
