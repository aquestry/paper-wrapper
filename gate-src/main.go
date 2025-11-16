package main

import (
	"fmt"

	"go.minekube.com/gate/cmd/gate"
)

func main() {
	fmt.Println("custom gate!")
	gate.Execute()
}
