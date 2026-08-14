//go:build unix

package unixsocket

import (
	"fmt"
	"strings"

	intercall "github.com/cerasos/intercall/go"
)

func validatePath(path string) error {
	if path == "" {
		return fmt.Errorf("unixsocket: empty socket path: %w", intercall.ErrInvalidArgument)
	}
	if strings.IndexByte(path, 0) >= 0 {
		return fmt.Errorf("unixsocket: socket path contains NUL: %w", intercall.ErrInvalidArgument)
	}
	if strings.HasPrefix(path, "@") {
		return fmt.Errorf("unixsocket: abstract socket path is not supported: %w", intercall.ErrInvalidArgument)
	}
	return nil
}
