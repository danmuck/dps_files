package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tui "github.com/danmuck/tui_go"
	logs "github.com/danmuck/smplog"
)

func executeDownloadAction(cfg RuntimeConfig, client *GRPCClient, input io.Reader) error {
	entries, err := client.List()
	if err != nil {
		return fmt.Errorf("list files: %w", err)
	}
	if len(entries) == 0 {
		logs.Println("No files on server.")
		return nil
	}

	t := cfg.TUI
	reader := getBufferedReader(input)
	t.InputLineFU("Tree options [-l N limit, Enter to skip]", "", true)
	logs.Printf("\n")
	optLine, _ := reader.ReadString('\n')
	_, _, limit := parseFSFlags(strings.TrimSpace(optLine))

	t.MenuTitleTC(&tui.TitleParams{Text: fmt.Sprintf("Stored files (%d)", len(entries))})
	nodes := buildRemoteTreeNodes(entries, limit)
	tvEntries := t.TreeViewTC(&tui.TreeViewParams{Nodes: nodes, ShowIndex: true})

	var selectedEntry RemoteFileEntry
	for {
		t.InputLineFU(fmt.Sprintf("Select file to download [0-%d] (or e to cancel)", len(tvEntries)-1), "", true)
		logs.Printf("\n")
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read selection: %w", err)
		}
		choice := strings.TrimSpace(line)
		if strings.EqualFold(choice, "e") {
			return errMenuBack
		}
		idx, convErr := strconv.Atoi(choice)
		if convErr != nil || idx < 0 || idx >= len(tvEntries) {
			t.StatusWarnFU(fmt.Sprintf("Invalid selection %q.", choice))
			logs.Printf("\n")
			continue
		}
		node := tvEntries[idx].Node.(remoteTreeNode)
		if node.IsEllipsis {
			t.StatusWarnFU("That entry is a placeholder — select a file or directory.")
			logs.Printf("\n")
			continue
		}
		selectedEntry = node.Entry
		break
	}

	// Directory: reassemble the full tree and return early
	if selectedEntry.IsDirectory() {
		manifestHash, err := hexToHash(selectedEntry.Hash)
		if err != nil {
			return fmt.Errorf("invalid directory hash %q: %w", selectedEntry.Hash, err)
		}
		outputDir := filepath.Join(cfg.KeyStore.StorageDir, selectedEntry.Name)
		logs.Printf("\nReassembling directory %q to %s\n", selectedEntry.Name, outputDir)
		summary := OpSummary{
			Operation: "download",
			FileName:  selectedEntry.Name,
			FileSize:  selectedEntry.Size,
			Timer:     tui.NewPhaseTimer(),
			StartedAt: time.Now(),
		}
		beginPhase(summary.Timer, summary.Operation, "reassemble", "reconstruct directory tree from server", 1, 1)
		reassembleErr := remoteReassembleDirectory(client, manifestHash, outputDir)
		summary.Timer.End()
		if reassembleErr != nil {
			summary.Err = reassembleErr
			renderSummary(t, summary)
			writeOpLog(summary)
			return fmt.Errorf("reassemble directory %q: %w", selectedEntry.Name, reassembleErr)
		}
		logs.Printf("Directory reassembled to %s\n", outputDir)
		renderSummary(t, summary)
		writeOpLog(summary)
		return nil
	}

	outputPath := filepath.Join(cfg.KeyStore.StorageDir, filepath.Base(selectedEntry.Name))
	logs.Printf("\nDownloading %q to %s\n", selectedEntry.Name, outputPath)

	summary := OpSummary{
		Operation: "download",
		FileName:  selectedEntry.Name,
		FileSize:  selectedEntry.Size,
		Timer:     tui.NewPhaseTimer(),
		StartedAt: time.Now(),
	}

	beginPhase(summary.Timer, summary.Operation, "download", "download file bytes from server", 1, 1)
	written, downloadErr := client.Download(selectedEntry.Name, outputPath, selectedEntry.Size)
	summary.Timer.End()

	summary.Bytes = written
	if downloadErr != nil {
		summary.Err = downloadErr
		renderSummary(t, summary)
		writeOpLog(summary)
		return fmt.Errorf("download %q: %w", selectedEntry.Name, downloadErr)
	}

	logs.Printf("Downloaded %s to %s\n", formatBytes(written), outputPath)
	renderSummary(t, summary)
	writeOpLog(summary)
	return nil
}

// remoteReassembleDirectory recursively downloads a directory tree from the remote server.
func remoteReassembleDirectory(c *GRPCClient, hash [32]byte, outputDir string) error {
	entries, err := c.ListDir(hash)
	if err != nil {
		return fmt.Errorf("list dir: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	for _, e := range entries {
		childOutput := filepath.Join(outputDir, e.Name)
		if e.Type == "directory" {
			if err := remoteReassembleDirectory(c, e.Hash, childOutput); err != nil {
				return fmt.Errorf("reassemble subdir %s: %w", e.Path, err)
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(childOutput), 0o755); err != nil {
				return fmt.Errorf("create parent dir: %w", err)
			}
			if _, err := c.DownloadByHash(e.Hash, childOutput); err != nil {
				return fmt.Errorf("download %s: %w", e.Path, err)
			}
		}
	}
	return nil
}
