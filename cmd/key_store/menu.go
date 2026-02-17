package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/danmuck/dps_files/src/key_store"
)

func isInteractiveInput(r *os.File) bool {
	info, err := r.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func promptAction(input io.Reader, cfg RuntimeConfig, indexedFiles []string, metadataCount int) (MenuAction, string, error) {
	if cfg.ActionProvided {
		return cfg.Action, fmt.Sprintf("%s (CLI)", cfg.Action), nil
	}

	if !isInteractiveInput(os.Stdin) {
		return cfg.Action, fmt.Sprintf("%s (non-interactive default)", cfg.Action), nil
	}

	reader := bufio.NewReader(input)
	for {
		fmt.Println("\nMain Menu")
		fmt.Println("  1) upload (store files from upload dir)")
		fmt.Println("  2) store (store explicit filepath)")
		fmt.Println("  3) clean (.kdht only)")
		fmt.Println("  4) deep clean (.kdht + metadata + cache)")
		fmt.Println("  5) view (inspect metadata + reassemble)")
		fmt.Printf("Choose action [1-5] (default: %s): ", cfg.Action)

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return cfg.Action, fmt.Sprintf("%s (EOF default)", cfg.Action), nil
			}
			return "", "", fmt.Errorf("failed to read action: %w", err)
		}

		choice := strings.ToLower(strings.TrimSpace(line))
		switch choice {
		case "", "1", string(ActionUpload), "u":
			if len(indexedFiles) == 0 {
				fmt.Printf("No indexed files are available under %s.\n", cfg.UploadDirectory)
				continue
			}
			return ActionUpload, "upload (from upload dir)", nil
		case "2", string(ActionStore), "s":
			return ActionStore, "store (explicit filepath)", nil
		case "3", string(ActionClean), "c":
			return ActionClean, "clean", nil
		case "4", string(ActionDeepClean), "deepclean", "deep_clean", "d":
			return ActionDeepClean, "deep clean", nil
		case "5", string(ActionView), "v":
			if metadataCount == 0 {
				fmt.Println("No metadata entries found in storage/metadata.")
				continue
			}
			return ActionView, "view", nil
		default:
			fmt.Printf("Invalid action %q. Enter 1-5 or action name.\n", choice)
		}
	}
}

func promptUploadSelection(indexedFiles []string, input io.Reader, cfg RuntimeConfig) ([]string, string, error) {
	if len(indexedFiles) == 0 {
		return nil, "", fmt.Errorf("no indexable files found in %s", cfg.UploadDirectory)
	}

	if cfg.RunAll {
		return append([]string(nil), indexedFiles...), "all indexed files (RUN_ALL=true)", nil
	}

	if cfg.DefaultFileIndex < 0 || cfg.DefaultFileIndex >= len(indexedFiles) {
		return nil, "", fmt.Errorf("default file index %d out of range for %d indexed files",
			cfg.DefaultFileIndex, len(indexedFiles))
	}

	if !isInteractiveInput(os.Stdin) {
		return []string{indexedFiles[cfg.DefaultFileIndex]},
			fmt.Sprintf("index %d (%q) [non-interactive default]", cfg.DefaultFileIndex, indexedFiles[cfg.DefaultFileIndex]), nil
	}

	fmt.Printf("\nUpload options from %s:\n", cfg.UploadDirectory)
	for idx, file := range indexedFiles {
		fmt.Printf("  %d) %s\n", idx, file)
	}

	reader := bufio.NewReader(input)
	for {
		fmt.Printf("\nSelect upload file [0-%d] or 'all' (default: %d): ", len(indexedFiles)-1, cfg.DefaultFileIndex)

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return []string{indexedFiles[cfg.DefaultFileIndex]},
					fmt.Sprintf("index %d (%q) [EOF default]", cfg.DefaultFileIndex, indexedFiles[cfg.DefaultFileIndex]), nil
			}
			return nil, "", fmt.Errorf("failed to read selection: %w", err)
		}

		choice := strings.TrimSpace(strings.ToLower(line))
		if choice == "" {
			return []string{indexedFiles[cfg.DefaultFileIndex]},
				fmt.Sprintf("index %d (%q)", cfg.DefaultFileIndex, indexedFiles[cfg.DefaultFileIndex]), nil
		}

		if choice == "all" || choice == "a" || choice == "*" {
			return append([]string(nil), indexedFiles...), fmt.Sprintf("all indexed files (%d)", len(indexedFiles)), nil
		}

		idx, convErr := strconv.Atoi(choice)
		if convErr != nil {
			fmt.Printf("Invalid selection %q. Enter a numeric index or 'all'.\n", choice)
			continue
		}

		if idx < 0 || idx >= len(indexedFiles) {
			fmt.Printf("Index %d out of range. Valid range is 0-%d.\n", idx, len(indexedFiles)-1)
			continue
		}

		return []string{indexedFiles[idx]}, fmt.Sprintf("index %d (%q)", idx, indexedFiles[idx]), nil
	}
}

func resolveStorePath(input io.Reader, cfg RuntimeConfig) (string, string, error) {
	if cfg.StoreFilePath != "" {
		cleaned := filepath.Clean(cfg.StoreFilePath)
		return cleaned, fmt.Sprintf("%s (CLI)", cleaned), nil
	}

	if !isInteractiveInput(os.Stdin) {
		return "", "", fmt.Errorf("store action requires %s PATH in non-interactive mode", STORE_PATH_FLAG)
	}

	reader := bufio.NewReader(input)
	for {
		fmt.Print("\nEnter file path to store: ")
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return "", "", fmt.Errorf("no file path provided")
			}
			return "", "", fmt.Errorf("failed to read file path: %w", err)
		}

		candidate := strings.TrimSpace(line)
		if candidate == "" {
			fmt.Println("Path cannot be empty.")
			continue
		}

		resolved := filepath.Clean(candidate)
		return resolved, resolved, nil
	}
}

func promptMetadataReassemblySelection(metadata []key_store.MetaData, input io.Reader) ([]key_store.MetaData, string, error) {
	if len(metadata) == 0 {
		return nil, "none", nil
	}

	if !isInteractiveInput(os.Stdin) {
		return nil, "none [non-interactive]", nil
	}

	reader := bufio.NewReader(input)
	for {
		fmt.Printf("\nReassemble which metadata entry [0-%d], 'all', or 'none' (default: none): ", len(metadata)-1)
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil, "none [EOF default]", nil
			}
			return nil, "", fmt.Errorf("failed to read view selection: %w", err)
		}

		choice := strings.ToLower(strings.TrimSpace(line))
		switch choice {
		case "", "none", "n":
			return nil, "none", nil
		case "all", "a", "*":
			return append([]key_store.MetaData(nil), metadata...), fmt.Sprintf("all metadata entries (%d)", len(metadata)), nil
		default:
			idx, convErr := strconv.Atoi(choice)
			if convErr != nil {
				fmt.Printf("Invalid selection %q. Enter numeric index, 'all', or 'none'.\n", choice)
				continue
			}
			if idx < 0 || idx >= len(metadata) {
				fmt.Printf("Index %d out of range. Valid range is 0-%d.\n", idx, len(metadata)-1)
				continue
			}
			return []key_store.MetaData{metadata[idx]}, fmt.Sprintf("index %d (%q)", idx, metadata[idx].FileName), nil
		}
	}
}
