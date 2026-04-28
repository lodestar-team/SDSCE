package plugin

// Register registers all SDS plugins with their respective packages.
// This should be called during init() in firehose-core.
//
// Usage in firehose-core config after registration:
//
//	common-auth-plugin: "sds://<plugin-gateway-host>:9003"
//	common-session-plugin: "sds://<plugin-gateway-host>:9003"
//	common-metering-plugin: "sds://<plugin-gateway-host>:9003?network=my-network"
//
// In the local demo stack, <plugin-gateway-host> is localhost and the
// private Plugin Gateway listens on :9003. Append ?plaintext=true only when
// that private endpoint is intentionally running plaintext.
// All three plugins connect to the same private Plugin Gateway endpoint.
// The sidecar handles all business logic (service provider, escrow, quotas, etc.).
func Register() {
	RegisterAuth()
	RegisterSession()
	RegisterMetering()
}
