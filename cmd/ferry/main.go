package main

import (
	"context"
	"os"

	"github.com/1etu/ferry/internal/app"
)

var version = "dev"

func main() {
	app.Version = version
	os.Exit(app.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
