// Package directives is a handwritten Go fixture for InterCall source
// directive and documentation parsing.
package directives

import "context"

// A no-parameter wire type.
// @intercall type user_id
type UserID uint64

// @intercall type name
type Name string

// A point on a plane.
// @intercall type point
type Point struct {
	// The horizontal coordinate.
	X float64 `intercall:"x"`
	// The vertical coordinate.
	Y float64 `intercall:"y"`
}

// SentinelError is the no-payload exception.
// @intercall exception sentinel_error
var SentinelError error

// @intercall exception
var PlainSentinel error

// ExPayload is the payload exception.
// @intercall exception ex_payload
type ExPayload struct {
	Message string `intercall:"message"`
}

// FindUser finds a user by name.
// @intercall procedure find_user
// @intercall param userID user_id
// @param userID the user to find
// @return the matching user
func FindUser(ctx context.Context, userID string) (User, error)

// @intercall procedure ping
func Ping(ctx context.Context) error

// LookupUser finds a user by id.
/* @intercall procedure lookup_user */
func LookupUser(ctx context.Context, id string) error
