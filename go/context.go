package intercall

import "context"

// connectionContextKey is the single private root-runtime context key. Only
// WithConnection and ConnectionFromContext use it, so a binding can never
// collide with a caller's or generated package's context values.
type connectionContextKey struct{}

// WithConnection returns a copy of ctx that carries conn under the private
// root-runtime key, replacing any earlier binding under that key. It follows
// context.WithValue: a nil parent or a nil connection panics.
func WithConnection(ctx context.Context, conn *Connection) context.Context {
	if ctx == nil {
		panic("cannot create context from nil parent")
	}
	if conn == nil {
		panic("cannot bind nil connection")
	}
	return context.WithValue(ctx, connectionContextKey{}, conn)
}

// ConnectionFromContext returns the nonnil connection bound by
// WithConnection. It returns ErrNoConnection when no connection is bound and
// ErrInvalidArgument for a nil context.
func ConnectionFromContext(ctx context.Context) (*Connection, error) {
	if ctx == nil {
		return nil, ErrInvalidArgument
	}
	conn, ok := ctx.Value(connectionContextKey{}).(*Connection)
	if !ok || conn == nil {
		return nil, ErrNoConnection
	}
	return conn, nil
}
