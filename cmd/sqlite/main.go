package main

import (
	"fmt"
	"os"

	"github.com/unaquatic/sqlite"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	fmt.Println("sqlite", sqlite.Version)
	return nil
}
