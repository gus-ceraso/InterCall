// Package directives is a fixture for the misplaced directive error.
package directives

import "context"

// @intercall type user_id
func F(ctx context.Context) error
