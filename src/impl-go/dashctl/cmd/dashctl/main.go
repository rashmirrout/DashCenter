// Command dashctl is the DashCenter operator CLI.
//
// Scaffold — Cobra wiring lives in internal/cmd and will be added as
// dashd's API surface stabilizes.
package main

import (
	"fmt"
	"os"
)

const version = "0.0.0-dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version") {
		fmt.Println("dashctl", version)
		return
	}
	fmt.Fprintf(os.Stderr, "dashctl %s (scaffold)\n", version)
	fmt.Fprintln(os.Stderr, "not yet implemented — see internal/cmd packages")
	os.Exit(0)
}
