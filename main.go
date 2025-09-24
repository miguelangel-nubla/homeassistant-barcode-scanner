package main

import (
	"fmt"
	"os"

	"github.com/miguelangel-nubla/homeassistant-barcode-scanner/pkg/cli"
)

func main() {
	cliApp := cli.NewCLI()
	if err := cliApp.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
