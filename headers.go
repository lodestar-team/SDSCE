package sds

// HTTP header names for SDS protocol
const (
	// HeaderRAV is the header containing the base64-encoded SignedRAV protobuf
	// Used by consumers to authenticate Substreams requests with providers
	HeaderRAV = "x-sds-rav"

	// HeaderSessionID is the header containing the session ID
	// Used to correlate requests with active payment sessions
	HeaderSessionID = "x-sds-session-id"
)
