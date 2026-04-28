package plugin

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	authv1 "github.com/graphprotocol/substreams-data-service/pb/graph/substreams/data_service/sds/auth/v1"
	"go.uber.org/zap"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func wrapAuthTransport(next http.Handler, logger *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, changed, err := sanitizeAuthRequestBody(r.Header.Get("Content-Type"), r.Body, logger)
		if err != nil {
			logger.Warn("failed to sanitize auth request body", zap.Error(err))
			http.Error(w, fmt.Sprintf("invalid auth request: %v", err), http.StatusBadRequest)
			return
		}

		if changed {
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
			r.Header.Set("Content-Length", strconv.Itoa(len(body)))
		}

		next.ServeHTTP(w, r)
	})
}

func sanitizeAuthRequestBody(contentType string, body io.ReadCloser, logger *zap.Logger) ([]byte, bool, error) {
	defer body.Close()

	rawBody, err := io.ReadAll(body)
	if err != nil {
		return nil, false, fmt.Errorf("read body: %w", err)
	}

	sanitized, changed, err := sanitizeAuthPayload(contentType, rawBody, logger)
	if err != nil {
		return nil, false, err
	}
	if !changed {
		return rawBody, false, nil
	}

	return sanitized, true, nil
}

func sanitizeAuthPayload(contentType string, payload []byte, logger *zap.Logger) ([]byte, bool, error) {
	if len(payload) == 0 {
		return payload, false, nil
	}

	if strings.HasPrefix(contentType, "application/grpc") {
		if len(payload) < 5 {
			return nil, false, fmt.Errorf("grpc frame too short")
		}
		if payload[0] != 0 {
			return nil, false, fmt.Errorf("grpc compression is not supported on auth sanitizer")
		}

		frameLen := int(payload[1])<<24 | int(payload[2])<<16 | int(payload[3])<<8 | int(payload[4])
		if frameLen != len(payload)-5 {
			return nil, false, fmt.Errorf("grpc frame length mismatch: frame=%d payload=%d", frameLen, len(payload)-5)
		}

		msg, changed, err := sanitizeValidateAuthMessage(payload[5:], logger)
		if err != nil {
			return nil, false, err
		}
		if !changed {
			return payload, false, nil
		}

		out := make([]byte, 5, 5+len(msg))
		out[0] = 0
		out[1] = byte(len(msg) >> 24)
		out[2] = byte(len(msg) >> 16)
		out[3] = byte(len(msg) >> 8)
		out[4] = byte(len(msg))
		out = append(out, msg...)
		return out, true, nil
	}

	msg, changed, err := sanitizeValidateAuthMessage(payload, logger)
	if err != nil {
		return nil, false, err
	}
	if !changed {
		return payload, false, nil
	}

	return msg, true, nil
}

func sanitizeValidateAuthMessage(payload []byte, logger *zap.Logger) ([]byte, bool, error) {
	original := payload
	req := &authv1.ValidateAuthRequest{
		UntrustedHeaders: map[string]*authv1.HeaderValues{},
	}

	var changed bool
	for len(payload) > 0 {
		num, typ, tagLen := protowire.ConsumeTag(payload)
		if tagLen < 0 {
			return nil, false, protowire.ParseError(tagLen)
		}
		payload = payload[tagLen:]

		switch num {
		case 1:
			if typ != protowire.BytesType {
				return nil, false, fmt.Errorf("unexpected wire type for untrusted_headers: %v", typ)
			}
			entryBytes, n := protowire.ConsumeBytes(payload)
			if n < 0 {
				return nil, false, protowire.ParseError(n)
			}
			key, values, entryChanged, err := sanitizeAuthHeaderEntry(entryBytes, logger)
			if err != nil {
				return nil, false, err
			}
			changed = changed || entryChanged
			if key != "" && len(values) > 0 {
				req.UntrustedHeaders[key] = &authv1.HeaderValues{Values: values}
			}
			payload = payload[n:]
		case 2:
			if typ != protowire.BytesType {
				return nil, false, fmt.Errorf("unexpected wire type for path: %v", typ)
			}
			pathBytes, n := protowire.ConsumeBytes(payload)
			if n < 0 {
				return nil, false, protowire.ParseError(n)
			}
			sanitizedPath := strings.ToValidUTF8(string(pathBytes), "")
			changed = changed || sanitizedPath != string(pathBytes)
			req.Path = sanitizedPath
			payload = payload[n:]
		case 3:
			if typ != protowire.BytesType {
				return nil, false, fmt.Errorf("unexpected wire type for ip_address: %v", typ)
			}
			ipBytes, n := protowire.ConsumeBytes(payload)
			if n < 0 {
				return nil, false, protowire.ParseError(n)
			}
			sanitizedIP := strings.ToValidUTF8(string(ipBytes), "")
			changed = changed || sanitizedIP != string(ipBytes)
			req.IpAddress = sanitizedIP
			payload = payload[n:]
		default:
			fieldLen := protowire.ConsumeFieldValue(num, typ, payload)
			if fieldLen < 0 {
				return nil, false, protowire.ParseError(fieldLen)
			}
			changed = true
			payload = payload[fieldLen:]
		}
	}

	if !changed {
		return original, false, nil
	}

	msg, err := proto.Marshal(req)
	if err != nil {
		return nil, false, fmt.Errorf("marshal sanitized auth request: %w", err)
	}
	return msg, true, nil
}

func sanitizeAuthHeaderEntry(payload []byte, logger *zap.Logger) (string, []string, bool, error) {
	var (
		key     string
		values  []string
		changed bool
	)

	for len(payload) > 0 {
		num, typ, tagLen := protowire.ConsumeTag(payload)
		if tagLen < 0 {
			return "", nil, false, protowire.ParseError(tagLen)
		}
		payload = payload[tagLen:]

		switch num {
		case 1:
			if typ != protowire.BytesType {
				return "", nil, false, fmt.Errorf("unexpected wire type for header key: %v", typ)
			}
			keyBytes, n := protowire.ConsumeBytes(payload)
			if n < 0 {
				return "", nil, false, protowire.ParseError(n)
			}
			sanitizedKey := strings.ToLower(strings.ToValidUTF8(string(keyBytes), ""))
			changed = changed || sanitizedKey != string(keyBytes)
			key = sanitizedKey
			payload = payload[n:]
		case 2:
			if typ != protowire.BytesType {
				return "", nil, false, fmt.Errorf("unexpected wire type for header values: %v", typ)
			}
			valueBytes, n := protowire.ConsumeBytes(payload)
			if n < 0 {
				return "", nil, false, protowire.ParseError(n)
			}
			sanitizedValues, valuesChanged, err := sanitizeAuthHeaderValues(valueBytes, logger)
			if err != nil {
				return "", nil, false, err
			}
			changed = changed || valuesChanged
			values = sanitizedValues
			payload = payload[n:]
		default:
			fieldLen := protowire.ConsumeFieldValue(num, typ, payload)
			if fieldLen < 0 {
				return "", nil, false, protowire.ParseError(fieldLen)
			}
			changed = true
			payload = payload[fieldLen:]
		}
	}

	if key == "" {
		return "", nil, changed, nil
	}
	if _, ok := forwardedAuthHeaderNames[key]; !ok {
		return "", nil, true, nil
	}
	return key, values, changed, nil
}

func sanitizeAuthHeaderValues(payload []byte, logger *zap.Logger) ([]string, bool, error) {
	values, changed, err := sanitizeAuthHeaderValuesMessage(payload, logger)
	if err == nil {
		return values, changed, nil
	}

	if !utf8.Valid(payload) {
		logger.Debug("dropping non-UTF8 auth header value from transport payload")
		return nil, true, nil
	}

	// Older firecore images serialized repeated Header{key,value} entries instead of
	// the newer map<string, HeaderValues> contract. In that legacy shape, the field 2
	// payload is the raw string value, not a nested HeaderValues message.
	return []string{string(payload)}, true, nil
}

func sanitizeAuthHeaderValuesMessage(payload []byte, logger *zap.Logger) ([]string, bool, error) {
	var (
		values  []string
		changed bool
	)

	for len(payload) > 0 {
		num, typ, tagLen := protowire.ConsumeTag(payload)
		if tagLen < 0 {
			return nil, false, protowire.ParseError(tagLen)
		}
		payload = payload[tagLen:]

		if num != 1 || typ != protowire.BytesType {
			return nil, false, fmt.Errorf("not header values encoding")
		}

		valueBytes, n := protowire.ConsumeBytes(payload)
		if n < 0 {
			return nil, false, protowire.ParseError(n)
		}
		payload = payload[n:]

		if !utf8.Valid(valueBytes) {
			changed = true
			logger.Debug("dropping non-UTF8 auth header value from transport payload")
			continue
		}

		values = append(values, string(valueBytes))
	}

	return values, changed, nil
}
