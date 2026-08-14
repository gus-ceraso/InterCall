package websocket

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	intercall "github.com/cerasos/intercall/go"
	coderws "github.com/coder/websocket"
)

func validateURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("websocket: empty URL: %w", intercall.ErrInvalidArgument)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("websocket: invalid URL: %w: %w", err, intercall.ErrInvalidArgument)
	}
	if (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" {
		return fmt.Errorf("websocket: URL must use ws or wss with a host: %w", intercall.ErrInvalidArgument)
	}
	return nil
}

func validateDialHeaders(header http.Header) error {
	for name := range header {
		if strings.EqualFold(name, "Connection") || strings.EqualFold(name, "Upgrade") || strings.HasPrefix(strings.ToLower(name), "sec-websocket-") {
			return fmt.Errorf("websocket: reserved handshake header %q: %w", name, intercall.ErrInvalidArgument)
		}
	}
	return nil
}

func validateDialArguments(ctx context.Context, raw string, options *DialOptions) error {
	if ctx == nil {
		return fmt.Errorf("websocket: nil dial context: %w", intercall.ErrInvalidArgument)
	}
	if err := validateURL(raw); err != nil {
		return err
	}
	if _, err := normalizeMessageLimit(options.MessageLimit); err != nil {
		return err
	}
	return validateDialHeaders(options.HTTPHeader)
}

func dialCore(httpCtx, streamCtx context.Context, raw string, options *DialOptions) (intercall.ByteStream, *http.Response, error) {
	conn, response, err := coderws.Dial(httpCtx, raw, options.coderOptions())
	if err != nil {
		return nil, response, fmt.Errorf("websocket: dial: %w", err)
	}
	stream, err := newStream(streamCtx, conn, options.MessageLimit)
	if err != nil {
		return nil, response, err
	}
	return stream, response, nil
}

// DialStream performs a WebSocket upgrade and returns its binary continuous
// byte stream plus the HTTP upgrade response. The context owns both setup
// and the returned stream.
func DialStream(ctx context.Context, raw string, options *DialOptions) (intercall.ByteStream, *http.Response, error) {
	copy := options.clone()
	if err := validateDialArguments(ctx, raw, copy); err != nil {
		return nil, nil, err
	}
	return dialCore(ctx, ctx, raw, copy)
}
