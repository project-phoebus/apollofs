package main

import (
	"os"

	"github.com/project-phoebus/apollofs/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
