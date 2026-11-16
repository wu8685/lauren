package main

import (
	"os"

	"github.com/wu8685/lauren/internal/core"
)

func main() {
	os.Exit(core.Run(os.Args[1:], os.Stdout, os.Stderr))
}
