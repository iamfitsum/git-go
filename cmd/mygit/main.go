package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/ini.v1"
)

func hashFile(fileContents []byte) (string, error) {
	header := fmt.Sprintf("blob %d\x00", len(fileContents))
	data := append([]byte(header), fileContents...)

	hash := fmt.Sprintf("%x", sha1.Sum(data))
	objectDir := fmt.Sprintf(".git/objects/%s", hash[:2])
	objectPath := fmt.Sprintf("%s/%s", objectDir, hash[2:])

	if _, err := os.Stat(objectPath); err == nil {
		return hash, nil
	}

	err := os.MkdirAll(objectDir, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("error creating directory: %w", err)
	}

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

	return hash, nil
}

func getFullHashFromAbbreviated(abbrev string) (string, error) {
	if len(abbrev) < 7 {
		return "", fmt.Errorf("abbreviated hash too short, must be at least 7 characters")
	}

	dir := fmt.Sprintf(".git/objects/%s", abbrev[:2])
	files, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("error reading object directory: %s", err)
	}

	for _, file := range files {
		if strings.HasPrefix(file.Name(), abbrev[2:]) {
			return file.Name(), nil
		}
	}

	return "", fmt.Errorf("could not resolve full hash from abbreviated: %s", abbrev)
}

func hexToBytes(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd length hex string")
	}
	b := make([]byte, len(s)/2)
	_, err := hex.Decode(b, []byte(s))
	if err != nil {
		return nil, err
	}
	return b, nil
}

func writeIndex(entries map[string]string) error {
	indexPath := filepath.Join(".git", "index")
	var buffer bytes.Buffer

	for path, hash := range entries {
		mode := "100644"
		buffer.WriteString(fmt.Sprintf("%s %s\x00", mode, path))
		hashBytes, err := hexToBytes(hash)
		if err != nil {
			return fmt.Errorf("error converting hash to bytes: %w", err)
		}
		buffer.Write(hashBytes)
	}

	return os.WriteFile(indexPath, buffer.Bytes(), 0644)
}

func readIndex() (map[string]string, error) {
	indexPath := filepath.Join(".git", "index")
	content, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}

	entries := make(map[string]string)
	for i := 0; i < len(content); {
		if i+6 > len(content) {
			break
		}
		i += 7

		pathStart := i
		nullIndex := bytes.IndexByte(content[i:], 0)
		if nullIndex == -1 {
			break
		}
		path := string(content[pathStart : i+nullIndex])
		i += nullIndex + 1

		if i+20 > len(content) {
			break
		}
		hashBytes := content[i : i+20]
		hash := hex.EncodeToString(hashBytes)
		i += 20

		entries[path] = hash
	}
	return entries, nil
}

func addFileToIndex(indexEntries map[string]string, filePath string) error {
	fileContents, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("error reading file '%s': %w", filePath, err)
	}
	hash, err := hashFile(fileContents)
	if err != nil {
		return fmt.Errorf("error hashing file '%s': %w", filePath, err)
	}
	indexEntries[filePath] = hash
	fmt.Printf("Added %s\n", filePath)
	return nil
}

func compress(data []byte) []byte {
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	w.Write(data)
	w.Close()
	return b.Bytes()
}

func getGitConfig() (name, email string, err error) {
	cfg, err := ini.Load(filepath.Join(".git", "config"))
	if err != nil {
		return "", "", fmt.Errorf("error reading .git/config: %w", err)
	}

	userSection, err := cfg.GetSection("user")
	if err != nil {
		return "", "", fmt.Errorf("no [user] section in .git/config")
	}

	nameKey, err := userSection.GetKey("name")
	if err != nil {
		return "", "", fmt.Errorf("no 'name' in [user] section")
	}

	emailKey, err := userSection.GetKey("email")
	if err != nil {
		return "", "", fmt.Errorf("no 'email' in [user] section")
	}

	return nameKey.String(), emailKey.String(), nil
}

func writeTreeFromIndex(indexEntries map[string]string) (string, error) {
	var buffer bytes.Buffer
	var entries []string

	for path, hash := range indexEntries {
		entries = append(entries, fmt.Sprintf("100644 %s\x00%s", path, hash))
	}
	sort.Strings(entries)

	for _, entry := range entries {
		parts := strings.SplitN(entry, "\x00", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid index entry format: %s", entry)
		}
		header := parts[0]
		hashStr := parts[1]
		hashBytes, err := hexToBytes(hashStr)
		if err != nil {
			return "", fmt.Errorf("error converting hex hash to bytes: %w", err)
		}
		buffer.WriteString(header)
		buffer.Write(hashBytes)
	}

	data := append([]byte(fmt.Sprintf("tree %d\x00", buffer.Len())), buffer.Bytes()...)
	treeHash := fmt.Sprintf("%x", sha1.Sum(data))

	objectDir := filepath.Join(".git", "objects", treeHash[:2])
	objectPath := filepath.Join(objectDir, treeHash[2:])
	os.MkdirAll(objectDir, 0755)

	err := os.WriteFile(objectPath, compress(data), 0644)
	if err != nil {
		return "", err
	}

	return treeHash, nil
}

func readCompressedObject(objectPath string) ([]byte, error) {
	objectFile, err := os.Open(objectPath)
	if err != nil {
		return nil, err
	}
	defer objectFile.Close()

	zr, err := zlib.NewReader(objectFile)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	return io.ReadAll(zr)
}

func extractTreeHashFromCommit(commitData string) string {
	lines := strings.SplitN(commitData, "\n", 2)
	if len(lines) > 0 && strings.HasPrefix(lines[0], "tree ") {
		return strings.TrimPrefix(lines[0], "tree ")
	}
	return ""
}

func compareTrees(oldTreeHash, newTreeHash string) (int, int) {
	oldEntries := readTreeEntries(oldTreeHash)
	newEntries := readTreeEntries(newTreeHash)

	insertions := 0
	deletions := 0

	oldPaths := make(map[string]string)
	for _, entry := range oldEntries {
		parts := strings.SplitN(entry, "\x00", 2)
		headerParts := strings.SplitN(parts[0], " ", 2)
		if len(headerParts) == 2 {
			oldPaths[headerParts[1]] = parts[1]
		}
	}

	newPaths := make(map[string]string)
	for _, entry := range newEntries {
		parts := strings.SplitN(entry, "\x00", 2)
		headerParts := strings.SplitN(parts[0], " ", 2)
		if len(headerParts) == 2 {
			newPaths[headerParts[1]] = parts[1]
		}
	}

	for path, newHash := range newPaths {
		if _, exists := oldPaths[path]; !exists {
			insertions++
		} else if oldPaths[path] != newHash {
			insertions++
			deletions++
		}
	}

	for path := range oldPaths {
		if _, exists := newPaths[path]; !exists {
			deletions++
		}
	}

	return insertions, deletions
}

func readTreeEntries(treeHash string) []string {
	if treeHash == "" {
		return nil
	}
	objectPath := filepath.Join(".git", "objects", treeHash[:2], treeHash[2:])
	compressedData, err := readCompressedObject(objectPath)
	if err != nil {
		return nil
	}

	nullIndex := bytes.IndexByte(compressedData, 0)
	if nullIndex == -1 {
		return nil
	}
	content := compressedData[nullIndex+1:]

	entries := []string{}
	i := 0
	for i < len(content) {
		nullIndex := bytes.IndexByte(content[i:], 0)
		if nullIndex == -1 {
			break
		}
		entry := string(content[i : i+nullIndex+21])
		entries = append(entries, entry)
		i += nullIndex + 21
	}
	return entries
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
	case "read-tree":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "usage: mygit read-tree <object>\n")
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

		data := out.Bytes()

		nullIndex := bytes.IndexByte(data, 0)
		if nullIndex == -1 {
			fmt.Fprintf(os.Stderr, "Invalid object format\n")
			os.Exit(1)
		}

		content := data[nullIndex+1:]

		fmt.Println("Tree Object Contents:")

		i := 0
		for i < len(content) {
			spaceIndex := bytes.IndexByte(content[i:], ' ')
			if spaceIndex == -1 {
				break
			}
			mode := string(content[i : i+spaceIndex])
			startOfPath := i + spaceIndex + 1

			nullIndex := bytes.IndexByte(content[startOfPath:], 0)
			if nullIndex == -1 {
				break
			}
			path := string(content[startOfPath : startOfPath+nullIndex])
			startOfHash := startOfPath + nullIndex + 1

			if startOfHash+20 > len(content) {
				break
			}
			hashBytes := content[startOfHash : startOfHash+20]
			hash := hex.EncodeToString(hashBytes)

			fmt.Printf("Mode: %s | Path: %s | Hash: %s\n", mode, path, hash)

			i = startOfHash + 20
		}
	case "write-tree":
		var buffer bytes.Buffer
		directories := make(map[string]bool)

		err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if path == ".git" {
				return filepath.SkipDir
			}

			if strings.HasPrefix(path, ".git/") {
				return nil
			}

			if path == "." {
				return nil
			}

			mode := "100644"

			if info.IsDir() {
				if _, exists := directories[path]; !exists {
					directories[path] = true
					mode = "40000"
					buffer.WriteString(fmt.Sprintf("%s %s\x00", mode, path))
					buffer.Write(make([]byte, 20))
				}
				return nil
			}

			fileContents, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("error reading file %s: %w", path, err)
			}

			fileHash, err := hashFile(fileContents)
			if err != nil {
				return fmt.Errorf("error hashing file %s: %w", path, err)
			}

			hashBytes, err := hexToBytes(fileHash)
			if err != nil {
				return fmt.Errorf("error converting hex to bytes: %w", err)
			}

			buffer.WriteString(fmt.Sprintf("%s %s\x00", mode, path))
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
	case "add":
		if len(os.Args) < 2 {
			fmt.Fprintf(os.Stderr, "usage: mygit add [<file>...]\n")
			os.Exit(1)
		}

		indexEntries, err := readIndex()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading index: %s\n", err)
			os.Exit(1)
		}

		filesToAdd := os.Args[2:]
		if len(filesToAdd) == 0 {
			fmt.Fprintf(os.Stderr, "usage: mygit add [<file>...]\n")
			os.Exit(1)
		}

		for _, pattern := range filesToAdd {
			if pattern == "." {
				err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
					if err != nil {
						return err
					}
					if info.IsDir() {
						return nil
					}
					if strings.HasPrefix(path, ".git/") || path == ".git" {
						return nil
					}
					return addFileToIndex(indexEntries, path)
				})
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error walking directory: %s\n", err)
					os.Exit(1)
				}
			} else {
				err := addFileToIndex(indexEntries, pattern)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error adding file '%s': %s\n", pattern, err)
				}
			}
		}

		err = writeIndex(indexEntries)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing index: %s\n", err)
			os.Exit(1)
		}
	case "commit":
		commitCmd := flag.NewFlagSet("commit", flag.ExitOnError)
		messageFlag := commitCmd.String("m", "", "commit message")
		err := commitCmd.Parse(os.Args[2:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing arguments: %s\n", err)
			os.Exit(1)
		}

		if *messageFlag == "" {
			fmt.Fprintf(os.Stderr, "Please provide a commit message using -m\n")
			os.Exit(1)
		}

		authorName, authorEmail, err := getGitConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting Git config: %s\n", err)
			os.Exit(1)
		}
		committerName := authorName
		committerEmail := authorEmail
		timestamp := time.Now().Unix()
		timezone := time.Now().Format("-0700")

		indexEntries, err := readIndex()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading index: %s\n", err)
			os.Exit(1)
		}

		if len(indexEntries) == 0 {
			fmt.Fprintf(os.Stderr, "No changes staged to commit\n")
			os.Exit(1)
		}

		treeHash, err := writeTreeFromIndex(indexEntries)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing tree from index: %s\n", err)
			os.Exit(1)
		}

		parentHash := ""
		headContent, err := os.ReadFile(".git/HEAD")
		if err == nil {
			headStr := strings.TrimSpace(string(headContent))
			if strings.HasPrefix(headStr, "ref: ") {
				refPath := strings.TrimSpace(strings.TrimPrefix(headStr, "ref:"))
				refContent, err := os.ReadFile(filepath.Join(".git", refPath))
				if err == nil {
					parentHash = strings.TrimSpace(string(refContent))
				}
			}
		}

		var commitContent strings.Builder
		commitContent.WriteString(fmt.Sprintf("tree %s\n", treeHash))
		if parentHash != "" {
			commitContent.WriteString(fmt.Sprintf("parent %s\n", parentHash))
		}
		commitContent.WriteString(fmt.Sprintf("author %s <%s> %d %s\n", authorName, authorEmail, timestamp, timezone))
		commitContent.WriteString(fmt.Sprintf("committer %s <%s> %d %s\n", committerName, committerEmail, timestamp, timezone))
		commitContent.WriteString("\n")
		commitContent.WriteString(*messageFlag)

		commitData := []byte(commitContent.String())
		header := fmt.Sprintf("commit %d\x00", len(commitData))
		fullCommitData := append([]byte(header), commitData...)

		commitHash := fmt.Sprintf("%x", sha1.Sum(fullCommitData))
		objectDir := filepath.Join(".git", "objects", commitHash[:2])
		objectPath := filepath.Join(objectDir, commitHash[2:])
		os.MkdirAll(objectDir, 0755)
		err = os.WriteFile(objectPath, compress(fullCommitData), 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing commit object: %s\n", err)
			os.Exit(1)
		}

		currentBranch := ""
		headContent, err = os.ReadFile(".git/HEAD")
		if err == nil {
			headStr := strings.TrimSpace(string(headContent))
			if strings.HasPrefix(headStr, "ref: refs/heads/") {
				currentBranch = strings.TrimPrefix(headStr, "ref: refs/heads/")
			}
		}

		shortCommitHash := commitHash[:7]

		var changesSummary string
		if parentHash == "" {
			changesSummary = fmt.Sprintf("%d insertions(+)", len(indexEntries))
		} else {
			parentTreeHash := ""
			parentCommitPath := filepath.Join(".git", "objects", parentHash[:2], parentHash[2:])
			parentCommitData, err := readCompressedObject(parentCommitPath)
			if err == nil {
				parentTreeHash = extractTreeHashFromCommit(string(parentCommitData))
			}
			insertions, deletions := compareTrees(parentTreeHash, treeHash)
			changesSummary = fmt.Sprintf("%d insertions(+), %d deletions(-)", insertions, deletions)
		}

		fmt.Printf("[%s %s] %s\n", currentBranch, shortCommitHash, *messageFlag)
		if changesSummary != "" {
			fmt.Println(changesSummary)
		}

		headContent, err = os.ReadFile(".git/HEAD")
		if err == nil {
			headStr := strings.TrimSpace(string(headContent))
			if strings.HasPrefix(headStr, "ref: ") {
				refPath := strings.TrimSpace(strings.TrimPrefix(headStr, "ref:"))
				refFilePath := filepath.Join(".git", refPath)
				err = os.WriteFile(refFilePath, []byte(commitHash+"\n"), 0644)
				if err != nil {
					if os.IsNotExist(err) {
						err = os.MkdirAll(filepath.Dir(refFilePath), 0755)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Error creating ref directory: %s\n", err)
							os.Exit(1)
						}
						err = os.WriteFile(refFilePath, []byte(commitHash+"\n"), 0644)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Error creating and updating ref: %s\n", err)
							os.Exit(1)
						}
						fmt.Println(commitHash)
					} else {
						fmt.Fprintf(os.Stderr, "Error updating ref: %s\n", err)
						os.Exit(1)
					}
				} else {
					fmt.Println(commitHash)
				}
			}
		}

		err = writeIndex(make(map[string]string))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error clearing index: %s\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}
