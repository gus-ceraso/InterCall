package websocket

import (
	"context"
	"fmt"
	"net/http"

	intercall "github.com/cerasos/intercall/go"
	coderws "github.com/coder/websocket"
)

// AcceptStream accepts a binary WebSocket request and returns a continuous
// byte stream. The explicit context owns the stream after upgrade; the request
// context is not used as the post-hijack lifetime.
func AcceptStream(ctx context.Context, w http.ResponseWriter, r *http.Request, options *AcceptOptions) (intercall.ByteStream, error) {
	if ctx == nil {
		return nil, fmt.Errorf("websocket: nil accept context: %w", intercall.ErrInvalidArgument)
	}
	if w == nil {
		return nil, fmt.Errorf("websocket: nil response writer: %w", intercall.ErrInvalidArgument)
	}
	if r == nil {
		return nil, fmt.Errorf("websocket: nil request: %w", intercall.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	copy := options.clone()
	if _, err := normalizeMessageLimit(copy.MessageLimit); err != nil {
		return nil, err
	}
	conn, err := coderws.Accept(w, r, copy.coderOptions())
	if err != nil {
		return nil, fmt.Errorf("websocket: accept: %w", err)
	}
	stream, err := newStream(ctx, conn, copy.MessageLimit)
	if err != nil {
		return nil, err
	}
	return stream, nil
}
