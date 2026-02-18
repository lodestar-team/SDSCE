package plugin

// Register registers all SDS plugins with their respective packages.
// This should be called during init() in firehose-core.
//
// Usage in firehose-core config after registration:
//
//	common-auth-plugin: "sds://localhost:9001?plaintext=true"
//	common-session-plugin: "sds://localhost:9001?plaintext=true"
//	common-metering-plugin: "sds://localhost:9001?plaintext=true&network=my-network"
//
// All three plugins connect to the same provider sidecar endpoint.
// The sidecar handles all business logic (service provider, escrow, quotas, etc.).
func Register() {
	RegisterAuth()
	RegisterSession()
	RegisterMetering()
}
