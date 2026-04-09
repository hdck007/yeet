package main

import (
	"fmt"
	"os"

	"github.com/hdck007/yeet/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "yeet: %v\n", err)
		os.Exit(1)
	}
}
