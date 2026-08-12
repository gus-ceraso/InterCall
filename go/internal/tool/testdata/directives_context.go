// Package directives is a fixture for the context parameter exclusion.
package directives

import "context"

// @intercall param ctx wire_name
func F(ctx context.Context, id string) error
