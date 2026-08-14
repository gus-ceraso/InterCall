// Package directives is a fixture for the malformed directive error.
package directives

import "context"

// @intercall procedure find a b
func F(ctx context.Context) error
