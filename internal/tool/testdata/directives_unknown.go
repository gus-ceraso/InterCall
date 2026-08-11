// Package directives is a fixture for the unknown directive error.
package directives

import "context"

// @intercall frobnicate
func F(ctx context.Context) error
