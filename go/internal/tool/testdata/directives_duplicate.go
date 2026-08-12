// Package directives is a fixture for the duplicate directive error.
package directives

import "context"

// @intercall procedure find
// @intercall procedure search
func F(ctx context.Context) error
