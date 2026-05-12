package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	nonInteractive := flag.Bool("non-interactive", false, "read config from environment variables instead of prompts")
	force := flag.Bool("force", false, "overwrite existing config file")
	output := flag.String("output", "configs/gateway.yaml", "output path for generated config file")
	flag.Parse()

	if *nonInteractive {
		if err := RunNonInteractive(*output, *force); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := RunInteractive(*output, *force); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
