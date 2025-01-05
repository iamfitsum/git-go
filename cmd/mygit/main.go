package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// hashFile hashes the content of a file, stores it as a Git blob object, and returns the hash.
func hashFile(fileContents []byte) (string, error) {
	header := fmt.Sprintf("blob %d\x00", len(fileContents))
	data := append([]byte(header), fileContents...)

	hash := fmt.Sprintf("%x", sha1.Sum(data))
	objectDir := fmt.Sprintf(".git/objects/%s", hash[:2])
	objectPath := fmt.Sprintf("%s/%s", objectDir, hash[2:])

	// Check if the object already exists
	if _, err := os.Stat(objectPath); err == nil {
		return hash, nil // If it exists, return the hash
	}

	// Create the object directory
	err := os.MkdirAll(objectDir, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("error creating directory: %w", err)
	}

	// Write the compressed object data
	objectFile, err := os.Create(objectPath)
	if err != nil {
		return "", fmt.Errorf("error creating file: %w", err)
	}
	defer objectFile.Close()

	zw := zlib.NewWriter(objectFile)
	_, err = zw.Write(data)
	if err != nil {
		return "", fmt.Errorf("error compressing file: %w", err)
	}
	zw.Close()

	return hash, nil // Return the hash
}

// getFullHashFromAbbreviated resolves a short (7-character) hash to the full hash.
func getFullHashFromAbbreviated(abbrev string) (string, error) {
	if len(abbrev) < 7 {
		return "", fmt.Errorf("abbreviated hash too short, must be at least 7 characters")
	}

	// Ensure the directory path exists
	dir := fmt.Sprintf(".git/objects/%s", abbrev[:2]) // First two characters for the directory
	files, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("error reading object directory: %s", err)
	}

	// Iterate through the files and find the matching file based on the suffix
	for _, file := range files {
		if strings.HasPrefix(file.Name(), abbrev[2:]) {
			return file.Name(), nil // Return the full hash if a match is found
		}
	}

	return "", fmt.Errorf("could not resolve full hash from abbreviated: %s", abbrev)
}

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

		hash, err := hashFile(fileContents)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error hashing file: %s\n", err)
			os.Exit(1)
		}

		fmt.Println(hash)
	case "ls-tree":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "usage: mygit ls-tree <object>\n")
			os.Exit(1)
		}

		object := os.Args[2]
		objectDir := object[:2]
		objectFileName := object[2:]

		objectPath := fmt.Sprintf(".git/objects/%s/%s", objectDir, objectFileName)

		objectFile, err := os.Open(objectPath)
		if err != nil {
			fullHash, err := getFullHashFromAbbreviated(object)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error resolving object hash: %s\n", err)
				os.Exit(1)
			}

			objectPath = fmt.Sprintf(".git/objects/%s/%s", objectDir, fullHash)
			objectFile, err = os.Open(objectPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error opening object file: %s\n", err)
				os.Exit(1)
			}
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
	case "write-tree":
		var buffer bytes.Buffer

		err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if path == "." || strings.HasPrefix(path, ".git/") {
				return nil
			}

			mode := "100644" // Default mode for files
			if info.IsDir() {
				mode = "40000" // Mode for directories
				return nil     // Ignore directories for now
			}

			fileContents, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("error reading file %s: %w", path, err)
			}
			hash, err := hashFile(fileContents)
			if err != nil {
				return fmt.Errorf("error hashing file %s: %w", path, err)
			}

			buffer.WriteString(fmt.Sprintf("%s %s\x00", mode, path))
			hashBytes, _ := hex.DecodeString(hash)
			buffer.Write(hashBytes)

			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error walking the directory: %s\n", err)
			os.Exit(1)
		}

		data := append([]byte(fmt.Sprintf("tree %d\x00", buffer.Len())), buffer.Bytes()...)
		treeHash := fmt.Sprintf("%x", sha1.Sum(data))

		objectDir := fmt.Sprintf(".git/objects/%s", treeHash[:2])
		objectPath := fmt.Sprintf("%s/%s", objectDir, treeHash[2:])

		os.MkdirAll(objectDir, os.ModePerm)

		objectFile, err := os.Create(objectPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating tree object: %s\n", err)
			os.Exit(1)
		}
		defer objectFile.Close()

		zw := zlib.NewWriter(objectFile)
		_, err = zw.Write(data)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error compressing tree object: %s\n", err)
			os.Exit(1)
		}
		zw.Close()

		fmt.Println(treeHash)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}
