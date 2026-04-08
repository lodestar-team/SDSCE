package plugin

// Register registers all SDS plugins with their respective packages.
// This should be called during init() in firehose-core.
//
// Usage in firehose-core config after registration:
//
//	common-auth-plugin: "sds://localhost:9003"
//	common-session-plugin: "sds://localhost:9003"
//	common-metering-plugin: "sds://localhost:9003?network=my-network"
//
// For local/demo-only plaintext, explicitly append ?plaintext=true.
// All three plugins connect to the same private Plugin Gateway endpoint.
// The sidecar handles all business logic (service provider, escrow, quotas, etc.).
func Register() {
	RegisterAuth()
	RegisterSession()
	RegisterMetering()
}
