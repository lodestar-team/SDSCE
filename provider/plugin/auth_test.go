package plugin

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	sds "github.com/graphprotocol/substreams-data-service"
	authv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/auth/v1"
	"github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/auth/v1/authv1connect"
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

func TestWrapAuthTransport_PreservesReadableBodyForValidRequest(t *testing.T) {
	logger := zap.NewNop()
	svc := &captureAuthService{}

	path, authHandler := authv1connect.NewAuthServiceHandler(svc)
	mux := http.NewServeMux()
	mux.Handle(path, wrapAuthTransport(authHandler, logger))

	server := httptest.NewServer(mux)
	defer server.Close()

	client := authv1connect.NewAuthServiceClient(server.Client(), server.URL)
	_, err := client.ValidateAuth(context.Background(), connect.NewRequest(&authv1.ValidateAuthRequest{
		UntrustedHeaders: map[string]*authv1.HeaderValues{
			"x-sds-rav":        {Values: []string{"rav-value"}},
			"x-sds-session-id": {Values: []string{"session-id"}},
		},
		Path:      "/sf.substreams.rpc.v2/Blocks",
		IpAddress: "127.0.0.1",
	}))
	require.NoError(t, err)

	require.Equal(t, 1, svc.calls)
	require.NotNil(t, svc.req)
	require.Equal(t, []string{"rav-value"}, svc.req.GetUntrustedHeaders()["x-sds-rav"].GetValues())
	require.Equal(t, []string{"session-id"}, svc.req.GetUntrustedHeaders()["x-sds-session-id"].GetValues())
	require.Equal(t, "/sf.substreams.rpc.v2/Blocks", svc.req.GetPath())
	require.Equal(t, "127.0.0.1", svc.req.GetIpAddress())
}

func TestWrapAuthTransport_RewritesLegacyPayloadBeforeNextHandler(t *testing.T) {
	logger := zap.NewNop()

	var (
		called bool
		req    authv1.ValidateAuthRequest
	)

	handler := wrapAuthTransport(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, int64(len(body)), r.ContentLength)
		require.NoError(t, proto.Unmarshal(body, &req))

		w.WriteHeader(http.StatusNoContent)
	}), logger)

	raw := encodeLegacyAuthHeader("x-sds-rav", []byte("rav-value"))
	raw = append(raw, encodeLegacyAuthHeader("x-sds-session-id", []byte("session-id"))...)

	httpReq := httptest.NewRequest(http.MethodPost, authv1connect.AuthServiceValidateAuthProcedure, bytes.NewReader(raw))
	httpReq.Header.Set("Content-Type", "application/proto")
	httpReq.ContentLength = int64(len(raw))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httpReq)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.True(t, called)
	require.Equal(t, []string{"rav-value"}, req.GetUntrustedHeaders()["x-sds-rav"].GetValues())
	require.Equal(t, []string{"session-id"}, req.GetUntrustedHeaders()["x-sds-session-id"].GetValues())
}

func TestWrapAuthTransport_RejectsMalformedPayload(t *testing.T) {
	logger := zap.NewNop()
	called := false

	handler := wrapAuthTransport(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}), logger)

	httpReq := httptest.NewRequest(http.MethodPost, authv1connect.AuthServiceValidateAuthProcedure, bytes.NewReader([]byte{0x00, 0x00, 0x00}))
	httpReq.Header.Set("Content-Type", "application/grpc+proto")

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httpReq)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.False(t, called)
	require.Contains(t, recorder.Body.String(), "invalid auth request")
}

type captureAuthService struct {
	req   *authv1.ValidateAuthRequest
	calls int
}

func (s *captureAuthService) ValidateAuth(_ context.Context, req *connect.Request[authv1.ValidateAuthRequest]) (*connect.Response[authv1.ValidateAuthResponse], error) {
	s.calls++
	s.req = proto.Clone(req.Msg).(*authv1.ValidateAuthRequest)
	return connect.NewResponse(&authv1.ValidateAuthResponse{}), nil
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
