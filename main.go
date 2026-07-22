package main

import "github.com/MChorfa/ggvalet/cmd"

// version is injected at build time via -ldflags "-X main.version=<tag>".
// It defaults to "dev" for untagged local builds.
var version = "dev"

func main() {
	cmd.Execute(version)
}
