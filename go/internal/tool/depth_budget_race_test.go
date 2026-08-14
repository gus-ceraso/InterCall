//go:build race

package tool

import "time"

// deepMaxStackBytes is the maximum Go stack the RM-11 and RM-13
// boundary subprocesses allow under the race detector. The race build
// multiplies every stack frame by roughly two to three, and the
// standard library's own recursive walks over the preflight-bounded
// boundary shapes grow with it: checking the 4,096-deep defined-type
// chain resolves each named reference through several go/types frames,
// which alone exceeds the 16 MiB normal-build budget under race. The
// 32 MiB budget keeps the measured normal-build band of just below
// 8 MiB, times the race multiplier, inside a two-times headroom while
// staying 32 times below the default one-gigabyte goroutine limit; any
// call-stack growth proportional to the type depth in the tool's own
// walks would still crash the subprocess. The normal build keeps the
// strict 16 MiB budget.
var deepMaxStackBytes = 32 << 20

// deepSubprocessTimeout is the wall-clock bound of one boundary
// subprocess under the race detector, where the same work takes
// several times longer on any machine. The subprocess binary enforces
// the test binary's own 600-second alarm, so the parent bound is a
// safety net that never fires before the subprocess's own failure
// reporting.
var deepSubprocessTimeout = 600 * time.Second
