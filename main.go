// Command lagdoc answers "why is my machine slow right now?".
package main

import (
	"os"

	"github.com/ag3ntsc4rn/lagdoc/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
