package main

import (
	"github.com/aeon022/missionctl-core/secure"
	"github.com/aeon022/taskctl/cmd"
)

func main() {
	secure.Init("taskctl") // owner-only files from the first byte
	cmd.Execute()
}
