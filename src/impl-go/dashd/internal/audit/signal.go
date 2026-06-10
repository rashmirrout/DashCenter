// signal-0 portability shim.
//
// On Unix, os.Process.Signal(syscall.Signal(0)) is the canonical
// "check liveness" idiom. On Windows the syscall.Signal type is the
// same import but ListProcess semantics differ — the cast still
// compiles, and on Windows it always returns nil (so isStaleLock
// errs on the side of "alive" and refuses to take the lock). That's
// the safe failure mode for an audit log.
package audit

import "syscall"

func syscallSignal(s int) syscall.Signal { return syscall.Signal(s) }
