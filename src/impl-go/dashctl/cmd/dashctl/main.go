// Command dashctl is the DashCenter operator CLI.
//
// Build with:
//
//	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w \
//	  -X main.version=$VERSION -X main.commit=$GIT_SHA -X main.buildDate=$DATE" \
//	  -o bin/dashctl ./cmd/dashctl
package main

import (
	"os"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/cmd"
)

// Linker-set build info. Defaults are populated for `go run` / unstamped builds.
var (
	version   = "0.1.0-dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	os.Exit(cmd.Execute(cmd.BuildInfo{
		Version: version,
		Commit:  commit,
		Date:    buildDate,
	}))
}
