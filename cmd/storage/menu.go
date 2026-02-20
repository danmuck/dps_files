package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/danmuck/dps_files/src/key_store"
)

var errMenuBack = errors.New("menu back")
var errMenuExit = errors.New("menu exit")

func isInteractiveInput(r *os.File) bool {
	info, err := r.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func isInteractiveReader(input io.Reader) bool {
	file, ok := input.(*os.File)
	if !ok {
		// Non-file readers (e.g. buffered wrappers) are treated as interactive.
		return true
	}
	return isInteractiveInput(file)
}

func getBufferedReader(input io.Reader) *bufio.Reader {
	if reader, ok := input.(*bufio.Reader); ok {
		return reader
	}
	return bufio.NewReader(input)
}

func promptAction(input io.Reader, cfg *RuntimeConfig, indexedFiles []string, metadataCount int) (MenuAction, string, error) {
	if cfg.ActionProvided {
		return cfg.Action, fmt.Sprintf("%s (CLI)", cfg.Action), nil
	}

	if !isInteractiveReader(input) {
		return cfg.Action, fmt.Sprintf("%s (non-interactive default)", cfg.Action), nil
	}

	reader := getBufferedReader(input)
	for {
		modeLabel := cfg.Mode
		if cfg.Mode == ModeRemote {
			if cfg.RemoteAddr != "" {
				modeLabel = fmt.Sprintf("remote @ %s", cfg.RemoteAddr)
			} else {
				modeLabel = "remote (no address)"
			}
		}
		fmt.Printf("\n   --[ dps_files | %s ]--\n\n", modeLabel)
		fmt.Println("  view (inspect metadata + reassemble)")
		fmt.Println("  store (chunk/store explicit filepath)")
		fmt.Println("  upload (chunk/store files from upload dir)")
		fmt.Println("  delete (remove a single stored file + chunks)")
		fmt.Println("  download (write a stored file to disk)")
		fmt.Println()
		fmt.Println("  verify (deep integrity scan of all chunks)")
		fmt.Println("  expire (sweep and remove TTL-expired files)")
		fmt.Println("  clean (.kdht only)")
		fmt.Println("  deep clean (.kdht + metadata + cache)")
		fmt.Println()
		fmt.Println("  stats (storage + system)")
		fmt.Println("  mode (toggle local / remote)")
		fmt.Println("  exit")
		fmt.Println()
		fmt.Printf("Choose action (default: %s): ", cfg.Action)

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return cfg.Action, fmt.Sprintf("%s (EOF default)", cfg.Action), nil
			}
			return "", "", fmt.Errorf("failed to read action: %w", err)
		}

		choice := strings.ToLower(strings.TrimSpace(line))
		switch choice {
		case "mode", "m":
			if err := handleModeToggle(reader, cfg); err != nil && !errors.Is(err, errMenuBack) {
				fmt.Printf("Mode toggle: %v\n", err)
			}
			continue

		case "", string(ActionView), "vi":
			if cfg.Mode != ModeRemote && metadataCount == 0 {
				fmt.Println("No metadata entries found in storage/metadata.")
				continue
			}
			return ActionView, "view", nil

		case string(ActionUpload), "u", "up":
			if len(indexedFiles) == 0 {
				fmt.Printf("No indexed files are available under %s.\n", cfg.UploadDirectory)
				continue
			}
			return ActionUpload, "upload (from upload dir)", nil

		case string(ActionDownload), "dl", "down", "stream", "st":
			if cfg.Mode != ModeRemote && metadataCount == 0 {
				fmt.Println("No stored files to download.")
				continue
			}
			return ActionDownload, "download", nil

		case string(ActionStore), "s":
			return ActionStore, "store (explicit filepath)", nil

		case string(ActionDelete), "del":
			if cfg.Mode != ModeRemote && metadataCount == 0 {
				fmt.Println("No stored files to delete.")
				continue
			}
			return ActionDelete, "delete", nil

		case string(ActionVerify), "ve":
			return ActionVerify, "verify", nil

		case string(ActionExpire), "exp", "ex":
			return ActionExpire, "expire", nil

		case string(ActionClean), "cl":
			return ActionClean, "clean", nil

		case string(ActionDeepClean), "deepclean", "deep_clean", "dc", "cleand":
			return ActionDeepClean, "deep clean", nil

		case string(ActionStats), "stat":
			return ActionStats, "stats", nil

		case "e", "exit", "q":
			return "", "", errMenuExit

		default:
			fmt.Printf("Invalid action %q.\n\n", choice)
			fmt.Println("  Action         Shorthand   Description")
			fmt.Println("  ─────────────────────────────────────────────────────────────")
			fmt.Println("  mode           m           	toggle local / remote")
			fmt.Println("  view           vi          	inspect metadata + reassemble")
			fmt.Println("  upload         u, up          	store files from upload dir")
			fmt.Println("  download       dl         		write a stored file to disk (legacy alias: stream)")
			fmt.Println("  store          s           	store explicit filepath")
			fmt.Println("  delete         del		     	remove a stored file + chunks")
			fmt.Println("  verify         ve          	deep integrity scan of all chunks")
			fmt.Println("  expire         exp, ex     	sweep and remove TTL-expired files")
			fmt.Println("  clean          cl          	remove .kdht chunk files only")
			fmt.Println("  deep clean     dc, cleand  	remove .kdht + metadata + cache")
			fmt.Println("  stats          stat	   		storage + system info")
			fmt.Println("  exit           e, q        	quit")
		}
	}
}

// handleModeToggle switches between ModeRun and ModeRemote.
// When switching to remote, prompts the user to select or enter a remote address.
func handleModeToggle(reader *bufio.Reader, cfg *RuntimeConfig) error {
	if cfg.Mode == ModeRemote {
		cfg.Mode = ModeRun
		cfg.RemoteAddr = ""
		fmt.Println("Switched to local mode.")
		return nil
	}
	addr, err := promptRemoteAddress(reader, *cfg)
	if err != nil {
		return err
	}
	cfg.Mode = ModeRemote
	cfg.RemoteAddr = addr
	fmt.Printf("Switched to remote mode @ %s\n", addr)
	return nil
}

// promptRemoteAddress displays known remotes and lets the user pick one or enter a custom address.
func promptRemoteAddress(reader *bufio.Reader, cfg RuntimeConfig) (string, error) {
	if len(cfg.KnownRemotes) > 0 {
		fmt.Println("\nKnown remotes:")
		for i, r := range cfg.KnownRemotes {
			fmt.Printf("  [%d] %s (%s)\n", i, r.Name, r.Address)
		}
		fmt.Printf("  [%d] Enter custom address\n", len(cfg.KnownRemotes))
	}
	fmt.Print("\nSelect remote or enter address directly: ")
	line, err := reader.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			return "", errMenuBack
		}
		return "", fmt.Errorf("read remote selection: %w", err)
	}
	choice := strings.TrimSpace(line)
	if strings.EqualFold(choice, "e") || choice == "" {
		return "", errMenuBack
	}
	// Try numeric index into known remotes
	if idx, convErr := strconv.Atoi(choice); convErr == nil {
		if idx >= 0 && idx < len(cfg.KnownRemotes) {
			return cfg.KnownRemotes[idx].Address, nil
		}
		// Index == len(KnownRemotes) means "enter custom" — fall through
	} else {
		// Non-integer input: treat as literal address
		return choice, nil
	}
	// Custom address prompt
	fmt.Print("Enter remote address (host:port): ")
	line, err = reader.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			return "", errMenuBack
		}
		return "", fmt.Errorf("read custom address: %w", err)
	}
	addr := strings.TrimSpace(line)
	if addr == "" {
		return "", errMenuBack
	}
	return addr, nil
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

	if !isInteractiveReader(input) {
		return []string{indexedFiles[cfg.DefaultFileIndex]},
			fmt.Sprintf("index %d (%q) [non-interactive default]", cfg.DefaultFileIndex, indexedFiles[cfg.DefaultFileIndex]), nil
	}

	fmt.Printf("\nUpload options from %s:\n", cfg.UploadDirectory)
	for idx, file := range indexedFiles {
		fmt.Printf("  %d) %s\n", idx, file)
	}

	reader := getBufferedReader(input)
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
		if choice == "e" {
			return nil, "", errMenuBack
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

	if !isInteractiveReader(input) {
		return "", "", fmt.Errorf("store action requires %s PATH in non-interactive mode", STORE_PATH_FLAG)
	}

	reader := getBufferedReader(input)
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
		if strings.EqualFold(candidate, "e") {
			return "", "", errMenuBack
		}

		resolved := filepath.Clean(candidate)
		return resolved, resolved, nil
	}
}

func promptMetadataReassemblySelection(metadata []key_store.MetaData, input io.Reader) ([]key_store.MetaData, string, error) {
	if len(metadata) == 0 {
		return nil, "none", nil
	}

	if !isInteractiveReader(input) {
		return nil, "none [non-interactive]", nil
	}

	reader := getBufferedReader(input)
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
		case "e":
			return nil, "", errMenuBack
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
