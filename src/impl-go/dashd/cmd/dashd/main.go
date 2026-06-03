// Command dashd is the DashCenter daemon.
//
// Scaffolding stub — the real wire-up lives in internal/* and lands as those
// packages get implemented. For now this binary prints its version and exits.
package main

import (
	"fmt"
	"os"
)

const version = "0.0.0-dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version") {
		fmt.Println("dashd", version)
		return
	}
	fmt.Fprintf(os.Stderr, "dashd %s (scaffold)\n", version)
	fmt.Fprintln(os.Stderr, "not yet implemented — see internal/* packages")
	os.Exit(0)
}
