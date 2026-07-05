package main

import "github.com/rockholla/gitspork/internal/cli"

var (
	version = "dev"
)

func main() {
	cli.Execute(version)
}
