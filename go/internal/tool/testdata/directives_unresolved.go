// Package directives is a fixture for the unresolved directive error.
package directives

import "context"

// @intercall param nope wire_name
func F(ctx context.Context, id string) error
