// Command intercall-go generates InterCall Go bindings.
//
// The command implements SPEC.md "Commands": export discovers tagged Go
// procedures in the active module or workspace and writes one export
// binding plus its owned interface file, and import reads one exact
// interface file and writes one import binding. Both commands follow
// the ordered validation, ownership, and safe-replacement rules of
// SPEC.md "One-file ownership and safe replacement", render source
// diagnostics with physical positions per SPEC.md "Diagnostics", and
// never mutate the filesystem before the write phase.
package main

import "os"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
