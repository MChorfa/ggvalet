package main

import (
	"os"
	"testing"
)

func TestMainVersion(t *testing.T) {
	version = "test"
	os.Args = []string{"glv", "--version"}

	// main() will run Execute with the --version flag.
	main()
}
