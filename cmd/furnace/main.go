package main

import (
	"os"

	"github.com/go-sum/furnace/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}
