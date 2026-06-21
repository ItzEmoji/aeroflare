package cmd

import (
	"fmt"
	"os"
)

func PrintError(msg string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
}

func PrintSuccess(msg string) {
	fmt.Println(msg)
}

func PrintInfo(msg string) {
	fmt.Println(msg)
}

func PrintWarning(msg string) {
	fmt.Println("Warning: " + msg)
}
