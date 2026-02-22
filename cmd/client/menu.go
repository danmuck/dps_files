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
	logs "github.com/danmuck/smplog"
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

func promptAction(input io.Reader, cfg *RuntimeConfig, metadataCount int) (MenuAction, string, error) {
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
		logs.Printf("\n")
		logs.Titlef("--[ dps_files | %s ]--\n\n", modeLabel)
		logs.Menuf("  view 		(inspect metadata + reassemble)\n")
		logs.Menuf("  upload 	(store file or directory by path)\n")
		logs.Menuf("  delete 	(remove a single stored file + chunks)\n")
		logs.Menuf("  download 	(write a stored file to disk)\n")
		logs.Printf("\n")
		logs.Menuf("  verify 	(deep integrity scan of all chunks)\n")
		logs.Menuf("  expire 	(sweep and remove TTL-expired files)\n")
		logs.Menuf("  clean 	(.kdht only)\n")
		logs.Menuf("  deep cln 	(.kdht + metadata + cache)\n")
		logs.Printf("\n")
		logs.Menuf("  stats 	(storage + system)\n")
		logs.Menuf("  mode 		(toggle local / remote)\n")
		logs.Menuf("  exit\n")
		logs.Printf("\n")
		logs.DividerRune(0, '=')
		logs.Promptf("\nChoose action (default: %s): ", cfg.Action)

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
				logs.Printf("Mode toggle: %v\n", err)
			}
			continue

		case "", string(ActionView), "vi":
			if cfg.Mode != ModeRemote && metadataCount == 0 {
				logs.StatusWarn("No metadata entries found in storage/metadata.")
				logs.Printf("\n")
				continue
			}
			return ActionView, "view", nil

		case string(ActionUpload), "u", "up":
			return ActionUpload, "upload", nil

		case string(ActionDownload), "dl", "down", "stream", "st":
			if cfg.Mode != ModeRemote && metadataCount == 0 {
				logs.StatusWarn("No stored files to download.")
				logs.Printf("\n")
				continue
			}
			return ActionDownload, "download", nil

		case string(ActionDelete), "del":
			if cfg.Mode != ModeRemote && metadataCount == 0 {
				logs.StatusWarn("No stored files to delete.")
				logs.Printf("\n")
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
			logs.Printf("Invalid action %q.\n\n", choice)
			logs.Divider(0)
			logs.Printf("\n")
			logs.KeyHint("m", "mode — toggle local / remote")
			logs.Printf("\n")
			logs.KeyHint("vi", "view — inspect metadata + reassemble")
			logs.Printf("\n")
			logs.KeyHint("u, up", "upload — store file or directory by path")
			logs.Printf("\n")
			logs.KeyHint("dl", "download — write a stored file to disk")
			logs.Printf("\n")
			logs.KeyHint("del", "delete — remove a stored file + chunks")
			logs.Printf("\n")
			logs.KeyHint("ve", "verify — deep integrity scan of all chunks")
			logs.Printf("\n")
			logs.KeyHint("exp, ex", "expire — sweep and remove TTL-expired files")
			logs.Printf("\n")
			logs.KeyHint("cl", "clean — remove .kdht chunk files only")
			logs.Printf("\n")
			logs.KeyHint("dc, cleand", "deep clean — remove .kdht + metadata + cache")
			logs.Printf("\n")
			logs.KeyHint("stat", "stats — storage + system info")
			logs.Printf("\n")
			logs.KeyHint("e, q", "exit — quit")
			logs.Printf("\n")
		}
	}
}

// handleModeToggle switches between ModeRun and ModeRemote.
// When switching to remote, prompts the user to select or enter a remote address.
func handleModeToggle(reader *bufio.Reader, cfg *RuntimeConfig) error {
	if cfg.Mode == ModeRemote {
		cfg.Mode = ModeRun
		cfg.RemoteAddr = ""
		logs.Println("Switched to local mode.")
		return nil
	}
	addr, err := promptRemoteAddress(reader, *cfg)
	if err != nil {
		return err
	}
	cfg.Mode = ModeRemote
	cfg.RemoteAddr = addr
	logs.Printf("Switched to remote mode @ %s\n", addr)
	return nil
}

// promptRemoteAddress displays known remotes and lets the user pick one or enter a custom address.
func promptRemoteAddress(reader *bufio.Reader, cfg RuntimeConfig) (string, error) {
	if len(cfg.KnownRemotes) > 0 {
		logs.Titlef("\nKnown remotes:\n")
		for i, r := range cfg.KnownRemotes {
			logs.Dataf("  [%d] %s (%s)\n", i, r.Name, r.Address)
		}
		logs.Dataf("  [%d] Enter custom address\n", len(cfg.KnownRemotes))
	}
	logs.Prompt("\nSelect remote or enter address directly: ")
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
	logs.Prompt("Enter remote address (host:port): ")
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

// promptUploadPath handles the unified upload command.
//
// If the user enters a non-empty path, it is stat'd and returned with isDir set appropriately.
// If the user presses Enter with no input, browse mode lists local/upload/ entries
// (files and [dir] subdirectories) for quick selection.
// Returns (path, isDir, nil) on success, or errMenuBack if the user types "e".
func promptUploadPath(input io.Reader, cfg RuntimeConfig) (path string, isDir bool, err error) {
	reader := getBufferedReader(input)

	for {
		logs.Promptf("\nEnter path [%s]: ", cfg.UploadDirectory)
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			if readErr == io.EOF {
				return "", false, fmt.Errorf("no path provided")
			}
			return "", false, fmt.Errorf("read path: %w", readErr)
		}

		candidate := strings.TrimSpace(line)
		if strings.EqualFold(candidate, "e") {
			return "", false, errMenuBack
		}

		if candidate != "" {
			resolved := filepath.Clean(candidate)
			info, statErr := os.Stat(resolved)
			if statErr != nil {
				logs.Printf("Path not found: %v. Try again or press Enter to browse.\n", statErr)
				continue
			}
			return resolved, info.IsDir(), nil
		}

		// Empty input: browse local/upload/.
		entries, listErr := getUploadDirEntries(cfg.UploadDirectory)
		if listErr != nil || len(entries) == 0 {
			logs.Printf("No entries found in %s. Enter a path manually.\n", cfg.UploadDirectory)
			continue
		}

		logs.Titlef("\n%s:\n", cfg.UploadDirectory)
		for i, e := range entries {
			if e.IsDir {
				logs.Dataf("  %d) [dir] %s/\n", i, e.Name)
			} else {
				logs.Dataf("  %d) %s\n", i, e.Name)
			}
		}
		logs.Promptf("\nSelect [0-%d] or 'all' (files only): ", len(entries)-1)

		selLine, selErr := reader.ReadString('\n')
		if selErr != nil {
			if selErr == io.EOF {
				return "", false, fmt.Errorf("no selection")
			}
			return "", false, fmt.Errorf("read selection: %w", selErr)
		}
		sel := strings.TrimSpace(strings.ToLower(selLine))
		if sel == "e" {
			return "", false, errMenuBack
		}
		if sel == "all" || sel == "a" || sel == "*" {
			// Sentinel: caller iterates all files in upload dir.
			return cfg.UploadDirectory, false, nil
		}

		idx, convErr := strconv.Atoi(sel)
		if convErr != nil || idx < 0 || idx >= len(entries) {
			logs.Printf("Invalid selection %q.\n", sel)
			continue
		}
		chosen := entries[idx]
		return filepath.Join(cfg.UploadDirectory, chosen.Name), chosen.IsDir, nil
	}
}

// confirmDirectoryUpload asks the user to confirm a recursive directory store.
// Returns true if confirmed (user typed "y" or "yes"), false otherwise.
func confirmDirectoryUpload(input io.Reader, dirPath string) (bool, error) {
	reader := getBufferedReader(input)
	logs.Promptf("Store directory %q recursively? [y/N]: ", dirPath)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	choice := strings.ToLower(strings.TrimSpace(line))
	return choice == "y" || choice == "yes", nil
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
		logs.Promptf("\nReassemble which metadata entry [0-%d], 'all', or 'none' (default: none): ", len(metadata)-1)
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
				logs.Printf("Invalid selection %q. Enter numeric index, 'all', or 'none'.\n", choice)
				continue
			}
			if idx < 0 || idx >= len(metadata) {
				logs.Printf("Index %d out of range. Valid range is 0-%d.\n", idx, len(metadata)-1)
				continue
			}
			return []key_store.MetaData{metadata[idx]}, fmt.Sprintf("index %d (%q)", idx, metadata[idx].FileName), nil
		}
	}
}
