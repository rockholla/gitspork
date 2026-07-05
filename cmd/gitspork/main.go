package main

import "github.com/rockholla/gitspork/v2/internal/cli"

var (
	version = "dev"
)

func main() {
	cli.Execute(version)
}
