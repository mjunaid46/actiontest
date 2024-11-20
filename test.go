package main

import (
	"fmt"
	"log"
	"path/filepath"
)

func main() {
	// Get all .c files in the current directory
	files, err := filepath.Glob("*.c") // Adjust the pattern as needed
	if err != nil {
		log.Fatal(err)
	}

	// Check if no files matched the pattern
	if len(files) == 0 {
		fmt.Println("No .c files found.")
		return
	}

	// Print each file name
	for _, file := range files {
		fmt.Println(file)
	}
}
