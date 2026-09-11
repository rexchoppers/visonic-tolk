package main

import (
	"fmt"
	"os"
)

var version = "dev"

func main() {
	if _, err := fmt.Fprintf(os.Stdout, "visonic-tolk %s\n", version); err != nil {
		os.Exit(1)
	}
}
