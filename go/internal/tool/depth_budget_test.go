//go:build !race

package tool

import "time"

// deepMaxStackBytes is the maximum Go stack the RM-11 and RM-13
// boundary subprocesses allow in normal builds. The export mapping
// recurses once per reachable named type with frames of a few
// kilobytes, and the measured failure band of the deepest boundary
// generation sits just below 8 MiB; the 16 MiB budget keeps that
// headroom while staying 64 times below the default one-gigabyte
// goroutine limit, so any call-stack growth proportional to the type
// depth would crash the subprocess.
var deepMaxStackBytes = 16 << 20

// deepSubprocessTimeout is the wall-clock bound of one boundary
// subprocess in normal builds: the deepest boundary generation
// completes in well under a minute, and 150 seconds absorbs cold-cache
// and load variance. The race build extends this bound.
var deepSubprocessTimeout = 150 * time.Second
