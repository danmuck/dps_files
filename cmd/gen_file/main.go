// gen_file generates random test files of a specified size.
//
// Usage:
//
//	go run cmd/gen_file/main.go <size> [filename]
//
// Size accepts suffixes: B, KB, MB, GB (e.g., "256MB", "1GB", "65536").
// If no filename is given, one is generated from the size.
// If the file already exists and matches the requested size, it is reused.
package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const DefaultUploadDir = "local/upload"

func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	multiplier := int64(1)

	switch {
	case strings.HasSuffix(s, "GB"):
		multiplier = 1 << 30
		s = strings.TrimSuffix(s, "GB")
	case strings.HasSuffix(s, "MB"):
		multiplier = 1 << 20
		s = strings.TrimSuffix(s, "MB")
	case strings.HasSuffix(s, "KB"):
		multiplier = 1 << 10
		s = strings.TrimSuffix(s, "KB")
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	}

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	return n * multiplier, nil
}

func defaultSizeLabel(size int64) string {
	switch {
	case size > 0 && size%(1<<30) == 0:
		return fmt.Sprintf("%dGB", size>>(30))
	case size > 0 && size%(1<<20) == 0:
		return fmt.Sprintf("%dMB", size>>(20))
	case size > 0 && size%(1<<10) == 0:
		return fmt.Sprintf("%dKB", size>>(10))
	default:
		return fmt.Sprintf("%dB", size)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: gen_file <size> [filename]\n")
		fmt.Fprintf(os.Stderr, "  size: number with optional suffix (B, KB, MB, GB)\n")
		fmt.Fprintf(os.Stderr, "  Examples: 1MB, 256MB, 65536\n")
		fmt.Fprintf(os.Stderr, "  Default output dir when filename omitted: %s/\n", DefaultUploadDir)
		os.Exit(1)
	}

	size, err := parseSize(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	filename := ""
	if len(os.Args) >= 3 {
		filename = os.Args[2]
	} else {
		filename = filepath.Join(DefaultUploadDir, fmt.Sprintf("test_%s.dat", defaultSizeLabel(size)))
	}

	// Create parent directory if needed
	dir := filepath.Dir(filename)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating directory: %v\n", err)
			os.Exit(1)
		}
	}

	// Reuse existing file if it matches the requested size
	if info, err := os.Stat(filename); err == nil {
		if info.Size() == size {
			fmt.Printf("Reusing existing file: %s (%d bytes)\n", filename, size)
			return
		}
		fmt.Printf("File exists but size mismatch (%d != %d), regenerating\n", info.Size(), size)
	}

	fmt.Printf("Generating %s (%d bytes)...\n", filename, size)

	f, err := os.Create(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	// Write in 4MB chunks for efficiency
	const chunkSize = 4 * 1024 * 1024
	buf := make([]byte, chunkSize)
	remaining := size

	for remaining > 0 {
		n := min(remaining, int64(chunkSize))
		if _, err := rand.Read(buf[:n]); err != nil {
			fmt.Fprintf(os.Stderr, "Error generating random data: %v\n", err)
			os.Exit(1)
		}
		if _, err := f.Write(buf[:n]); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
			os.Exit(1)
		}
		remaining -= n
	}

	fmt.Printf("Generated: %s (%d bytes)\n", filename, size)
}
