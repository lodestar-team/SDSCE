package gateway

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/streamingfast/dauth"
	"go.uber.org/zap"
)

// trustedHeadersInterceptor extracts trusted headers from incoming HTTP requests
// (those with "X-Sf-" prefix) and adds them to the request context using dauth.
type trustedHeadersInterceptor struct {
	logger *zap.Logger
}

func newTrustedHeadersInterceptor(logger *zap.Logger) connect.Interceptor {
	return &trustedHeadersInterceptor{
		logger: logger.Named("trusted-headers-interceptor"),
	}
}

func (i *trustedHeadersInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		ctx = i.extractTrustedHeaders(ctx, req.Header())
		return next(ctx, req)
	}
}

func (i *trustedHeadersInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next // Client-side interceptor not needed
}

func (i *trustedHeadersInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx = i.extractTrustedHeaders(ctx, conn.RequestHeader())
		return next(ctx, conn)
	}
}

// extractTrustedHeaders extracts trusted headers from the HTTP request
// and adds them to the context as trusted headers.
//
// Supports two prefixes:
//   - "X-Sf-" (StreamingFast/dauth headers): strips prefix and uses remainder as key
//   - "x-sds-" (SDS-specific headers): uses full header name as key
func (i *trustedHeadersInterceptor) extractTrustedHeaders(ctx context.Context, headers map[string][]string) context.Context {
	trustedHeaders := make(dauth.TrustedHeaders)

	i.logger.Debug("extracting trusted headers", zap.Int("total_headers", len(headers)))

	for key, values := range headers {
		if len(values) == 0 {
			continue
		}

		lowerKey := strings.ToLower(key)

		// Check for StreamingFast/dauth headers (X-Sf- prefix)
		if strings.HasPrefix(lowerKey, "x-sf-") {
			// Extract the header name without the "X-Sf-" prefix
			headerName := key[5:] // Remove "X-Sf-" prefix
			trustedHeaders[headerName] = values[0]
			i.logger.Debug("found trusted header (X-Sf- prefix)",
				zap.String("key", key),
				zap.String("extracted_name", headerName),
				zap.String("value", values[0]),
			)
		} else if strings.HasPrefix(lowerKey, "x-sds-") {
			// SDS-specific headers - use full header name
			trustedHeaders[lowerKey] = values[0]
			i.logger.Debug("found trusted header (x-sds- prefix)",
				zap.String("key", lowerKey),
				zap.String("value", values[0]),
			)
		}
	}

	if len(trustedHeaders) == 0 {
		i.logger.Warn("no trusted headers found in request")
		return ctx
	}

	i.logger.Debug("adding trusted headers to context",
		zap.Int("count", len(trustedHeaders)),
		zap.Strings("header_names", trustedHeaders.Names()),
	)

	return dauth.WithTrustedHeaders(ctx, trustedHeaders)
}
