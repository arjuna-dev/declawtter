package main

import (
	"fmt"
	"os"

	"declaw/internal/app"
)

func main() {
	application, err := app.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	code := application.Run(os.Args[1:])
	os.Exit(code)
}
