package main

import (
	"os"

	"github.com/hdck007/yeet/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
