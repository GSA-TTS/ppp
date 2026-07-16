// Command ppp is the entrypoint for the Podman Plus Proxy CLI.
//
// It is deliberately thin: all command wiring lives in internal/cli so the
// binary's main package stays free of business logic.
package main

import (
	"os"

	"github.com/GSA-TTS/ppp/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		// Cobra already prints the error to stderr; just set the exit code.
		os.Exit(1)
	}
}
