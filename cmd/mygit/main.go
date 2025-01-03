package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"strings"
)

// Usage: your_program.sh <command> <arg1> <arg2> ...
func main() {
	// You can use print statements as follows for debugging, they'll be visible when running tests.
	// fmt.Fprintf(os.Stderr, "Logs from your program will appear here!\n")

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: mygit <command> [<args>...]\n")
		os.Exit(1)
	}

	switch command := os.Args[1]; command {
	case "init":
		for _, dir := range []string{".git", ".git/objects", ".git/refs"} {
			if err := os.MkdirAll(dir, 0755); err != nil {
				fmt.Fprintf(os.Stderr, "Error creating directory: %s\n", err)
				os.Exit(1)
			}
		}

		headFileContents := []byte("ref: refs/heads/main\n")
		if err := os.WriteFile(".git/HEAD", headFileContents, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing file: %s\n", err)
			os.Exit(1)
		}

		fmt.Println("Initialized git directory")
	case "cat-file":
		if len(os.Args) < 4 {
			fmt.Fprintf(os.Stderr, "usage: mygit cat-file -p <object>\n")
			os.Exit(1)
		}
		
		flag := os.Args[2]
		if flag != "-p" {
			fmt.Fprintf(os.Stderr, "usage: mygit cat-file -p <object>\n")
			os.Exit(1)
		}

		object := os.Args[3]
		objectPath := fmt.Sprintf(".git/objects/%s/%s", object[:2], object[2:])
		objectFile, err := os.Open(objectPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening file: %s\n", err)
			os.Exit(1)
		}
		defer objectFile.Close()
		
		zr, err := zlib.NewReader(objectFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating zlib reader: %s\n", err)
			os.Exit(1)
		}
		defer zr.Close()
		
		var out bytes.Buffer
		_, err = io.Copy(&out, zr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error decompressing file: %s\n", err)
			os.Exit(1)
		}
		
		data := out.String()
		nullIndex := strings.IndexByte(data, 0)
		if nullIndex == -1 {
			fmt.Fprintf(os.Stderr, "Invalid object format\n")
			os.Exit(1)
		}
		
		content := data[nullIndex+1:]
		fmt.Print(content)
	case "hash-object":
		if len(os.Args) < 4 {
			fmt.Fprintf(os.Stderr, "usage: mygit hash-object -w <file>\n")
			os.Exit(1)
		}
		
		flag := os.Args[2]
		if flag != "-w" {
			fmt.Fprintf(os.Stderr, "usage: mygit hash-object -w <file>\n")
			os.Exit(1)
		}
		
		file := os.Args[3]
		fileContents := []byte{}
		if file != "" {
			var err error
			fileContents, err = os.ReadFile(file)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading file: %s\n", err)
				os.Exit(1)
			}
		}
	
		header := fmt.Sprintf("blob %d\x00", len(fileContents))
		data := append([]byte(header), fileContents...)
	
		hash := fmt.Sprintf("%x", sha1.Sum(data))
		objectDir := fmt.Sprintf(".git/objects/%s", hash[:2])
		objectPath := fmt.Sprintf("%s/%s", objectDir, hash[2:])
		
		err := os.MkdirAll(objectDir, os.ModePerm)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating directory: %s\n", err)
			os.Exit(1)
		}
			
		objectFile, err := os.Create(objectPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating file: %s\n", err)
			os.Exit(1)
		}
		defer objectFile.Close()
	
		zw := zlib.NewWriter(objectFile)
		_, err = zw.Write(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error compressing file: %s\n", err)
			os.Exit(1)
		}
		zw.Close()
	
		fmt.Println(hash)	
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}
