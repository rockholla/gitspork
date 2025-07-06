package main

import "github.com/rockholla/gitspork/cmd"

var (
	version = "dev"
)

func main() {
	cmd.Execute(version)
}
