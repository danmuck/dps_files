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
var errMenuRefresh = errors.New("menu refresh")

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
			switchErr := handleServerSwitch(reader, cfg)
			if switchErr == nil {
				// Successfully switched — exit promptAction so the outer loop
				// can reconnect and refresh metadataCount before re-entering menu.
				return cfg.Action, "server switch", errMenuRefresh
			}
			if !errors.Is(switchErr, errMenuBack) {
				logs.Printf("Server switch: %v\n", switchErr)
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

// parseFSFlags tokenises input, extracts -d N (depth) and -l N (per-dir limit),
// and returns the remaining tokens joined as the path.
// Defaults: depth=5, limit=0 (unlimited).
func parseFSFlags(input string) (path string, depth, limit int) {
	depth = 5
	tokens := strings.Fields(input)
	var pathTokens []string
	for i := 0; i < len(tokens); i++ {
		switch tokens[i] {
		case "-d":
			if i+1 < len(tokens) {
				if n, err := strconv.Atoi(tokens[i+1]); err == nil && n >= 0 {
					depth = n
				}
				i++
			}
		case "-l":
			if i+1 < len(tokens) {
				if n, err := strconv.Atoi(tokens[i+1]); err == nil && n > 0 {
					limit = n
				}
				i++
			}
		default:
			pathTokens = append(pathTokens, tokens[i])
		}
	}
	path = strings.Join(pathTokens, " ")
	return path, depth, limit
}

// promptUploadPath handles the unified upload command.
func promptUploadPath(input io.Reader, cfg RuntimeConfig) (path string, isDir bool, err error) {
	t := cfg.TUI
	nl := func() { logs.Printf("\n") }
	reader := getBufferedReader(input)

	for {
		t.InputLineFU(fmt.Sprintf("Enter path [%s]  (-d N depth, -l N limit)", cfg.UploadDirectory), "", true)
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

		rawPath, depth, limit := parseFSFlags(candidate)

		if rawPath != "" {
			resolved := filepath.Clean(expandPath(rawPath))
			info, statErr := os.Stat(resolved)
			if statErr != nil {
				logs.Printf("Path not found: %v. Try again or press Enter to browse.\n", statErr)
				continue
			}
			if !info.IsDir() {
				return resolved, false, nil
			}
			// Directory path entered: show tree and let user pick.
			selectedPath, selectedIsDir, selErr := selectFromLocalFSTree(t, reader, resolved, depth, limit)
			if errors.Is(selErr, errMenuBack) {
				continue
			}
			return selectedPath, selectedIsDir, selErr
		}

		// Empty input: browse cfg.UploadDirectory.
		uploadDir := filepath.Clean(expandPath(cfg.UploadDirectory))
		selectedPath, selectedIsDir, selErr := selectFromLocalFSTree(t, reader, uploadDir, depth, limit)
		if errors.Is(selErr, errMenuBack) {
			continue
		}
		return selectedPath, selectedIsDir, selErr
	}
}

// selectFromLocalFSTree renders a tree view of the local filesystem at rootPath
// and returns the selected path, whether it's a directory, and any error.
func selectFromLocalFSTree(t tui.TUI, reader *bufio.Reader, rootPath string, maxDepth, limit int) (string, bool, error) {
	nodes, err := buildLocalFSTreeNodes(rootPath, maxDepth, limit)
	if err != nil {
		return "", false, fmt.Errorf("browse %s: %w", rootPath, err)
	}
	t.MenuTitleTC(&tui.TitleParams{Text: rootPath})
	tvEntries := t.TreeViewTC(&tui.TreeViewParams{Nodes: nodes, ShowIndex: true})

	for {
		t.InputLineFU(fmt.Sprintf("Select [0-%d], 'all' to upload all files, or 'e' to cancel", len(tvEntries)-1), "", true)
		logs.Printf("\n")
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
			// Sentinel: caller iterates all files in rootPath.
			return rootPath, false, nil
		}
		idx, convErr := strconv.Atoi(sel)
		if convErr != nil || idx < 0 || idx >= len(tvEntries) {
			logs.Printf("Invalid selection %q.\n", sel)
			logs.Printf("\n")
			continue
		}
		node := tvEntries[idx].Node.(localFSTreeNode)
		if node.IsEllipsis {
			logs.Printf("That entry is a placeholder — select a file or directory.\n")
			logs.Printf("\n")
			continue
		}
		return node.Path, node.IsDir, nil
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
