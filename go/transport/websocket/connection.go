package websocket

import (
	"context"
	"fmt"
	"time"

	intercall "github.com/cerasos/intercall/go"
)

const defaultPhaseTimeout = 10 * time.Second

func validateNegotiatedBindings(ctx context.Context, export intercall.ExportBinding, imp intercall.ImportBinding) error {
	if ctx == nil {
		return fmt.Errorf("websocket: nil connection context: %w", intercall.ErrInvalidArgument)
	}
	if export == (intercall.ExportBinding{}) {
		return fmt.Errorf("websocket: zero export binding: %w", intercall.ErrInvalidArgument)
	}
	if imp == (intercall.ImportBinding{}) {
		return fmt.Errorf("websocket: zero import binding: %w", intercall.ErrInvalidArgument)
	}
	if _, ok := export.InterfaceID(); !ok {
		return fmt.Errorf("websocket: export binding has no interface ID: %w", intercall.ErrInvalidArgument)
	}
	if _, ok := imp.InterfaceID(); !ok {
		return fmt.Errorf("websocket: import binding has no interface ID: %w", intercall.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// Dial establishes a WebSocket, agrees on the expected peer interfaces, and
// returns the resulting InterCall connection. The original context owns the
// connection after the HTTP dial phase completes.
func Dial(ctx context.Context, raw string, export intercall.ExportBinding, imp intercall.ImportBinding) (*intercall.Connection, error) {
	if err := validateNegotiatedBindings(ctx, export, imp); err != nil {
		return nil, err
	}
	if err := validateURL(raw); err != nil {
		return nil, err
	}
	options := (&DialOptions{}).clone()
	if err := validateDialArguments(ctx, raw, options); err != nil {
		return nil, err
	}
	dialCtx, cancel := context.WithTimeout(ctx, defaultPhaseTimeout)
	defer cancel()
	stream, _, err := dialCore(dialCtx, ctx, raw, options)
	if err != nil {
		return nil, err
	}
	conn, err := intercall.NewNegotiatedClientConnection(ctx, stream, export, imp)
	if err != nil {
		_ = stream.Close()
		return nil, err
	}
	return conn, nil
}
