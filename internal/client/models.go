package client

// CreateJavaServerRequest follows the verified Crafty 4.10.4 Java download contract.
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
// Crafty 4.10.4 download_jar multiplies each memory input by 1000 for JVM M flags.
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
	ID        string  `json:"server_id"`
	Name      *string `json:"server_name"`
	AutoStart *bool   `json:"auto_start"`
}

// UpdateServerRequest contains the name update supported by this provider.
// Crafty 4.10.4 accepts additional configuration fields; see docs/api-contract.md.
type UpdateServerRequest struct {
	Name string `json:"server_name"`
}
