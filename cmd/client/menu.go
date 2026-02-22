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

	logs "github.com/danmuck/smplog"
	tui "github.com/danmuck/tui_go"
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
		nl()
		tuiCfg := tui.Configured()
		origTitle := tuiCfg.Colors.Title
		tuiCfg.Colors.Title = logs.StyleColor256(logs.Magenta)
		tui.Configure(tuiCfg)
		t.MenuTitleTC(&tui.TitleParams{Text: fmt.Sprintf("dps_files | %s", cfg.ServerLabel)})
		tuiCfg.Colors.Title = origTitle
		tui.Configure(tuiCfg)

		t.MenuTC(&tui.MenuParams{Items: []tui.MenuEntry{
			{Label: "view      inspect stored files"},
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
			{Label: "server    switch active server"},
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
		case "server", "s", "m":
			if err := handleServerSwitch(reader, cfg); err != nil && !errors.Is(err, errMenuBack) {
				logs.Printf("Server switch: %v\n", err)
			}
			continue

		case "", string(ActionView), "vi":
			if metadataCount == 0 {
				t.StatusWarnFU("No files found on server.")
				nl()
				continue
			}
			return ActionView, "view", nil

		case string(ActionUpload), "u", "up":
			return ActionUpload, "upload", nil

		case string(ActionDownload), "dl", "down", "stream", "st":
			if metadataCount == 0 {
				t.StatusWarnFU("No stored files to download.")
				nl()
				continue
			}
			return ActionDownload, "download", nil

		case string(ActionDelete), "del":
			if metadataCount == 0 {
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
			t.KeyHintFU("s", "server — switch active server")
			nl()
			t.KeyHintFU("vi", "view — inspect stored files")
			nl()
			t.KeyHintFU("u, up", "upload — store file or directory by path")
			nl()
			t.KeyHintFU("dl", "download — write a stored file to disk")
			nl()
			t.KeyHintFU("del", "delete — remove a stored file + chunks")
			nl()
			t.KeyHintFU("ve", "verify — deep integrity scan of all chunks")
			nl()
			t.KeyHintFU("exp, ex", "expire — sweep and remove TTL-expired files")
			nl()
			t.KeyHintFU("cl", "clean — remove .kdht chunk files only")
			nl()
			t.KeyHintFU("dc, cleand", "deep clean — remove .kdht + metadata + cache")
			nl()
			t.KeyHintFU("stat", "stats — storage + system info")
			nl()
			t.KeyHintFU("e, q", "exit — quit")
			nl()
		}
	}
}

// handleServerSwitch prompts for a new server address and updates cfg.
func handleServerSwitch(reader *bufio.Reader, cfg *RuntimeConfig) error {
	addr, err := promptRemoteAddress(reader, cfg)
	if err != nil {
		return err
	}
	cfg.ServerAddr = addr
	cfg.ServerLabel = fmt.Sprintf("remote @ %s", addr)
	clearTerminalIfInteractive(os.Stdin)
	logs.Printf("Switched to server %s\n", addr)
	return nil
}

// promptRemoteAddress displays known remotes and lets the user pick one or enter a custom address.
func promptRemoteAddress(reader *bufio.Reader, cfg *RuntimeConfig) (string, error) {
	t := cfg.TUI
	nl := func() { logs.Printf("\n") }

	if len(cfg.KnownRemotes) > 0 {
		t.MenuTitleTC(&tui.TitleParams{Text: "Known remotes"})
		for i, r := range cfg.KnownRemotes {
			t.FieldFU(fmt.Sprintf("[%d] %s", i, r.Name), r.Address)
			nl()
		}
		t.FieldFU(fmt.Sprintf("[%d]", len(cfg.KnownRemotes)), "Enter custom address")
		nl()
	}
	t.InputLineFU("Select remote or enter address directly", "", true)
	nl()
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
	t.InputLineFU("Enter remote address (host:port)", "", true)
	nl()
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
		t.InputLineFU(fmt.Sprintf("Enter path [%s]", cfg.UploadDirectory), "", true)
		nl()
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
				t.FieldFU(fmt.Sprintf("%d", i), fmt.Sprintf("[dir] %s/", e.Name))
				nl()
			} else {
				t.FieldFU(fmt.Sprintf("%d", i), e.Name)
				nl()
			}
		}
		t.InputLineFU(fmt.Sprintf("Select [0-%d] or 'all' (files only)", len(entries)-1), "", true)
		nl()

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
	cfg.TUI.InputLineFU(fmt.Sprintf("Store directory %q recursively? [Y/n]", dirPath), "", true)
	nl()
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	choice := strings.ToLower(strings.TrimSpace(line))
	return choice != "n" && choice != "no", nil
}
