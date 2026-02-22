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
	tui "github.com/danmuck/tui_go"
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

	t := cfg.TUI
	nl := func() { logs.Printf("\n") }
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
		nl()
		t.MenuTitleTC(&tui.TitleParams{Text: fmt.Sprintf("dps_files | %s", modeLabel)})
		t.MenuTC(&tui.MenuParams{Items: []tui.MenuEntry{
			{Label: "view      inspect metadata + reassemble"},
			{Label: "upload    store file or directory by path"},
			{Label: "delete    remove a single stored file + chunks"},
			{Label: "download  write a stored file to disk"},
		}})
		nl()
		t.MenuTC(&tui.MenuParams{Items: []tui.MenuEntry{
			{Label: "verify    deep integrity scan of all chunks"},
			{Label: "expire    sweep and remove TTL-expired files"},
			{Label: "clean     .kdht only"},
			{Label: "deep cln  .kdht + metadata + cache"},
		}})
		nl()
		t.MenuTC(&tui.MenuParams{Items: []tui.MenuEntry{
			{Label: "stats     storage + system"},
			{Label: "mode      toggle local / remote"},
			{Label: "exit"},
		}})
		nl()
		t.DividerTC(&tui.DividerParams{Rune: '='})
		t.InputLineFU(fmt.Sprintf("Choose action (default: %s)", string(cfg.Action)), "", true)
		nl()

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
				t.StatusWarnFU("No metadata entries found in storage/metadata.")
				nl()
				continue
			}
			return ActionView, "view", nil

		case string(ActionUpload), "u", "up":
			return ActionUpload, "upload", nil

		case string(ActionDownload), "dl", "down", "stream", "st":
			if cfg.Mode != ModeRemote && metadataCount == 0 {
				t.StatusWarnFU("No stored files to download.")
				nl()
				continue
			}
			return ActionDownload, "download", nil

		case string(ActionDelete), "del":
			if cfg.Mode != ModeRemote && metadataCount == 0 {
				t.StatusWarnFU("No stored files to delete.")
				nl()
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
			t.DividerTC(&tui.DividerParams{})
			nl()
			t.KeyHintFU("m", "mode — toggle local / remote"); nl()
			t.KeyHintFU("vi", "view — inspect metadata + reassemble"); nl()
			t.KeyHintFU("u, up", "upload — store file or directory by path"); nl()
			t.KeyHintFU("dl", "download — write a stored file to disk"); nl()
			t.KeyHintFU("del", "delete — remove a stored file + chunks"); nl()
			t.KeyHintFU("ve", "verify — deep integrity scan of all chunks"); nl()
			t.KeyHintFU("exp, ex", "expire — sweep and remove TTL-expired files"); nl()
			t.KeyHintFU("cl", "clean — remove .kdht chunk files only"); nl()
			t.KeyHintFU("dc, cleand", "deep clean — remove .kdht + metadata + cache"); nl()
			t.KeyHintFU("stat", "stats — storage + system info"); nl()
			t.KeyHintFU("e, q", "exit — quit"); nl()
		}
	}
}

// handleModeToggle switches between ModeRun and ModeRemote.
func handleModeToggle(reader *bufio.Reader, cfg *RuntimeConfig) error {
	if cfg.Mode == ModeRemote {
		cfg.Mode = ModeRun
		cfg.RemoteAddr = ""
		clearTerminalIfInteractive(os.Stdin)
		logs.Println("Switched to local mode.")
		return nil
	}
	clearTerminalIfInteractive(os.Stdin)
	addr, err := promptRemoteAddress(reader, cfg)
	if err != nil {
		return err
	}
	cfg.Mode = ModeRemote
	cfg.RemoteAddr = addr
	clearTerminalIfInteractive(os.Stdin)
	logs.Printf("Switched to remote mode @ %s\n", addr)
	return nil
}

// promptRemoteAddress displays known remotes and lets the user pick one or enter a custom address.
func promptRemoteAddress(reader *bufio.Reader, cfg *RuntimeConfig) (string, error) {
	t := cfg.TUI
	nl := func() { logs.Printf("\n") }

	if len(cfg.KnownRemotes) > 0 {
		t.MenuTitleTC(&tui.TitleParams{Text: "Known remotes"})
		for i, r := range cfg.KnownRemotes {
			t.FieldFU(fmt.Sprintf("[%d] %s", i, r.Name), r.Address); nl()
		}
		t.FieldFU(fmt.Sprintf("[%d]", len(cfg.KnownRemotes)), "Enter custom address"); nl()
	}
	t.InputLineFU("Select remote or enter address directly", "", true); nl()
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
	t.InputLineFU("Enter remote address (host:port)", "", true); nl()
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
func promptUploadPath(input io.Reader, cfg RuntimeConfig) (path string, isDir bool, err error) {
	t := cfg.TUI
	nl := func() { logs.Printf("\n") }
	reader := getBufferedReader(input)

	for {
		t.InputLineFU(fmt.Sprintf("Enter path [%s]", cfg.UploadDirectory), "", true); nl()
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

		t.MenuTitleTC(&tui.TitleParams{Text: cfg.UploadDirectory})
		for i, e := range entries {
			if e.IsDir {
				t.FieldFU(fmt.Sprintf("%d", i), fmt.Sprintf("[dir] %s/", e.Name)); nl()
			} else {
				t.FieldFU(fmt.Sprintf("%d", i), e.Name); nl()
			}
		}
		t.InputLineFU(fmt.Sprintf("Select [0-%d] or 'all' (files only)", len(entries)-1), "", true); nl()

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
			return filepath.Clean(cfg.UploadDirectory), false, nil
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
func confirmDirectoryUpload(input io.Reader, cfg RuntimeConfig, dirPath string) (bool, error) {
	nl := func() { logs.Printf("\n") }
	reader := getBufferedReader(input)
	cfg.TUI.InputLineFU(fmt.Sprintf("Store directory %q recursively? [Y/n]", dirPath), "", true); nl()
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	choice := strings.ToLower(strings.TrimSpace(line))
	return choice != "n" && choice != "no", nil
}

func promptMetadataReassemblySelection(t tui.TUI, metadata []key_store.MetaData, input io.Reader) ([]key_store.MetaData, string, error) {
	if len(metadata) == 0 {
		return nil, "none", nil
	}

	if !isInteractiveReader(input) {
		return nil, "none [non-interactive]", nil
	}

	nl := func() { logs.Printf("\n") }
	reader := getBufferedReader(input)
	for {
		t.InputLineFU(fmt.Sprintf("Reassemble which metadata entry [0-%d], 'all', or 'none' (default: none)", len(metadata)-1), "", true); nl()
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
