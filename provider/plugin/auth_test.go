package plugin

import (
	"testing"

	sds "github.com/graphprotocol/substreams-data-service"
	authv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/auth/v1"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestForwardedAuthHeaders_FiltersToSDSHeadersAndValidUTF8(t *testing.T) {
	logger := zap.NewNop()

	headers := map[string][]string{
		sds.HeaderRAV:                  {"rav-value"},
		sds.HeaderSessionID:            {"session-id"},
		"Grpc-Trace-Bin":               {string([]byte{0xff, 0xfe, 0xfd})},
		"X-Trace-Id":                   {"trace-id"},
		"X-Unrelated-Header":           {"ignore-me"},
		"X-SDS-Session-ID-Binary-Like": {string([]byte{0xff})},
	}

	got := forwardedAuthHeaders(headers, logger)

	require.Len(t, got, 3)
	require.Equal(t, []string{"rav-value"}, got["x-sds-rav"].GetValues())
	require.Equal(t, []string{"session-id"}, got["x-sds-session-id"].GetValues())
	require.Equal(t, []string{"trace-id"}, got["x-trace-id"].GetValues())
	require.NotContains(t, got, "grpc-trace-bin")
	require.NotContains(t, got, "x-unrelated-header")
}

func TestForwardedAuthHeaders_DropsInvalidUTF8FromAllowedHeaders(t *testing.T) {
	logger := zap.NewNop()

	headers := map[string][]string{
		sds.HeaderRAV:       {"rav-value", string([]byte{0xff, 0xfe})},
		sds.HeaderSessionID: {string([]byte{0xff})},
	}

	got := forwardedAuthHeaders(headers, logger)

	require.Equal(t, []string{"rav-value"}, got["x-sds-rav"].GetValues())
	require.NotContains(t, got, "x-sds-session-id")
}

func TestSanitizeValidateAuthMessage_FiltersHeadersAndDropsInvalidUTF8(t *testing.T) {
	logger := zap.NewNop()

	raw := append([]byte{},
		encodeAuthMapEntry("x-sds-rav", encodeHeaderValues([][]byte{[]byte("rav-value")}))...,
	)
	raw = append(raw, encodeAuthMapEntry("grpc-trace-bin", encodeHeaderValues([][]byte{{0xff, 0xfe}}))...)
	raw = append(raw, encodeAuthMapEntry("x-sds-session-id", encodeHeaderValues([][]byte{[]byte("session-id"), {0xff}}))...)
	raw = append(raw, encodeStringField(2, []byte("/sf.substreams.rpc.v2/Blocks"))...)
	raw = append(raw, encodeStringField(3, []byte("127.0.0.1"))...)

	sanitized, changed, err := sanitizeValidateAuthMessage(raw, logger)
	require.NoError(t, err)
	require.True(t, changed)

	var req authv1.ValidateAuthRequest
	require.NoError(t, proto.Unmarshal(sanitized, &req))
	require.Equal(t, []string{"rav-value"}, req.GetUntrustedHeaders()["x-sds-rav"].GetValues())
	require.Equal(t, []string{"session-id"}, req.GetUntrustedHeaders()["x-sds-session-id"].GetValues())
	require.NotContains(t, req.GetUntrustedHeaders(), "grpc-trace-bin")
	require.Equal(t, "/sf.substreams.rpc.v2/Blocks", req.GetPath())
	require.Equal(t, "127.0.0.1", req.GetIpAddress())
}

func TestSanitizeAuthPayload_GrpcFrame(t *testing.T) {
	logger := zap.NewNop()

	msg := encodeAuthMapEntry("grpc-trace-bin", encodeHeaderValues([][]byte{{0xff}}))
	msg = append(msg, encodeAuthMapEntry("x-sds-rav", encodeHeaderValues([][]byte{[]byte("rav-value")}))...)

	frame := make([]byte, 5, 5+len(msg))
	frame[0] = 0
	frame[1] = byte(len(msg) >> 24)
	frame[2] = byte(len(msg) >> 16)
	frame[3] = byte(len(msg) >> 8)
	frame[4] = byte(len(msg))
	frame = append(frame, msg...)

	sanitized, changed, err := sanitizeAuthPayload("application/grpc+proto", frame, logger)
	require.NoError(t, err)
	require.True(t, changed)
	require.Len(t, sanitized, 5+len(sanitized[5:]))
	require.Equal(t, byte(0), sanitized[0])

	msgLen := int(sanitized[1])<<24 | int(sanitized[2])<<16 | int(sanitized[3])<<8 | int(sanitized[4])
	require.Equal(t, len(sanitized)-5, msgLen)

	var req authv1.ValidateAuthRequest
	require.NoError(t, proto.Unmarshal(sanitized[5:], &req))
	require.Equal(t, []string{"rav-value"}, req.GetUntrustedHeaders()["x-sds-rav"].GetValues())
	require.NotContains(t, req.GetUntrustedHeaders(), "grpc-trace-bin")
}

func TestSanitizeValidateAuthMessage_LegacyHeaderEncoding(t *testing.T) {
	logger := zap.NewNop()

	raw := encodeLegacyAuthHeader("x-sds-rav", []byte("rav-value"))
	raw = append(raw, encodeLegacyAuthHeader("x-sds-session-id", []byte("session-id"))...)

	sanitized, changed, err := sanitizeValidateAuthMessage(raw, logger)
	require.NoError(t, err)
	require.True(t, changed)

	var req authv1.ValidateAuthRequest
	require.NoError(t, proto.Unmarshal(sanitized, &req))
	require.Equal(t, []string{"rav-value"}, req.GetUntrustedHeaders()["x-sds-rav"].GetValues())
	require.Equal(t, []string{"session-id"}, req.GetUntrustedHeaders()["x-sds-session-id"].GetValues())
}

func encodeAuthMapEntry(key string, headerValues []byte) []byte {
	entry := encodeStringField(1, []byte(key))
	entry = append(entry, encodeBytesField(2, headerValues)...)
	return encodeBytesField(1, entry)
}

func encodeLegacyAuthHeader(key string, value []byte) []byte {
	entry := encodeStringField(1, []byte(key))
	entry = append(entry, encodeStringField(2, value)...)
	return encodeBytesField(1, entry)
}

func encodeHeaderValues(values [][]byte) []byte {
	var out []byte
	for _, value := range values {
		out = append(out, encodeStringField(1, value)...)
	}
	return out
}

func encodeStringField(fieldNumber protowire.Number, value []byte) []byte {
	return encodeBytesField(fieldNumber, value)
}

func encodeBytesField(fieldNumber protowire.Number, value []byte) []byte {
	var out []byte
	out = protowire.AppendTag(out, fieldNumber, protowire.BytesType)
	out = protowire.AppendBytes(out, value)
	return out
}
