package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/danmuck/dps_files/src/key_store"
	tui "github.com/danmuck/tui_go"
	logs "github.com/danmuck/smplog"
)

func executeRemoteDownloadAction(cfg RuntimeConfig, input io.Reader) error {
	client, err := NewGRPCClient(cfg.RemoteAddr)
	if err != nil {
		return fmt.Errorf("connect to remote: %w", err)
	}
	defer client.Close()

	entries, err := client.List()
	if err != nil {
		return fmt.Errorf("list remote files: %w", err)
	}
	if len(entries) == 0 {
		logs.Println("No files on remote server.")
		return nil
	}

	t := cfg.TUI
	t.MenuTitleTC(&tui.TitleParams{Text: fmt.Sprintf("Remote files (%d)", len(entries))})
	nodes := buildRemoteTreeNodes(entries)
	tvEntries := t.TreeViewTC(&tui.TreeViewParams{Nodes: nodes, ShowIndex: true})

	reader := getBufferedReader(input)
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
		selectedEntry = tvEntries[idx].Node.(remoteTreeNode).Entry
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
			Operation: "remote-download",
			FileName:  selectedEntry.Name,
			FileSize:  selectedEntry.Size,
			Timer:     tui.NewPhaseTimer(),
			StartedAt: time.Now(),
		}
		beginPhase(summary.Timer, summary.Operation, "reassemble", "reconstruct directory tree from remote", 1, 1)
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
		Operation: "remote-download",
		FileName:  selectedEntry.Name,
		FileSize:  selectedEntry.Size,
		Timer:     tui.NewPhaseTimer(),
		StartedAt: time.Now(),
	}

	beginPhase(summary.Timer, summary.Operation, "download", "download file bytes from remote server", 1, 1)
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

func executeDownloadAction(cfg RuntimeConfig, ks *key_store.KeyStore, input io.Reader) error {
	if cfg.Mode == ModeRemote {
		return executeRemoteDownloadAction(cfg, input)
	}
	metadata := ks.ListKnownFiles()
	if len(metadata) == 0 {
		logs.Println("No stored files to download.")
		return nil
	}

	t := cfg.TUI
	t.MenuTitleTC(&tui.TitleParams{Text: fmt.Sprintf("Stored files (%d)", len(metadata))})
	nodes := buildLocalTreeNodes(metadata)
	tvEntries := t.TreeViewTC(&tui.TreeViewParams{Nodes: nodes, ShowIndex: true})

	reader := getBufferedReader(input)

	// Select file
	var selectedMD key_store.MetaData
	for {
		t.InputLineFU(fmt.Sprintf("Select file to download [0-%d] (or e to cancel)", len(tvEntries)-1), "", true)
		logs.Printf("\n")
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("failed to read selection: %w", err)
		}

		choice := strings.TrimSpace(line)
		if strings.EqualFold(choice, "e") {
			return errMenuBack
		}

		idx, convErr := strconv.Atoi(choice)
		if convErr != nil {
			t.StatusWarnFU(fmt.Sprintf("Invalid selection %q. Enter a numeric index or e.", choice))
			logs.Printf("\n")
			continue
		}
		if idx < 0 || idx >= len(tvEntries) {
			t.StatusWarnFU(fmt.Sprintf("Index %d out of range. Valid range is 0-%d.", idx, len(tvEntries)-1))
			logs.Printf("\n")
			continue
		}

		selectedMD = tvEntries[idx].Node.(localTreeNode).MD
		break
	}

	// Directory: reassemble the full tree and return early
	if selectedMD.IsDirectory() {
		outputDir := filepath.Join(cfg.KeyStore.StorageDir, selectedMD.FileName)
		logs.Printf("\nReassembling directory %q to %s\n", selectedMD.FileName, outputDir)
		summary := OpSummary{
			Operation: "local-download",
			FileName:  selectedMD.FileName,
			FileSize:  selectedMD.TotalSize,
			Timer:     tui.NewPhaseTimer(),
			StartedAt: time.Now(),
		}
		beginPhase(summary.Timer, summary.Operation, "reassemble", "reconstruct directory tree", 1, 1)
		reassembleErr := ks.ReassembleDirectory(selectedMD.FileHash, outputDir)
		summary.Timer.End()
		if reassembleErr != nil {
			summary.Err = reassembleErr
			renderSummary(t, summary)
			writeOpLog(summary)
			return fmt.Errorf("reassemble directory: %w", reassembleErr)
		}
		logs.Printf("Directory reassembled to %s\n", outputDir)
		renderSummary(t, summary)
		writeOpLog(summary)
		return nil
	}

	// Optional chunk range
	totalChunks := selectedMD.TotalBlocks
	var chunkStart, chunkEnd uint32
	useRange := false

	t.InputLineFU(fmt.Sprintf("Chunk range (total: %d chunks). Enter start end (e.g. '0 10') or press Enter for full file", totalChunks), "", true)
	logs.Printf("\n")
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to read chunk range: %w", err)
	}
	rangeInput := strings.TrimSpace(line)
	if rangeInput != "" && !strings.EqualFold(rangeInput, "e") {
		parts := strings.Fields(rangeInput)
		if len(parts) == 2 {
			start64, e1 := strconv.ParseUint(parts[0], 10, 32)
			end64, e2 := strconv.ParseUint(parts[1], 10, 32)
			if e1 != nil || e2 != nil {
				logs.Println("Invalid range; downloading full file instead.")
			} else {
				chunkStart = uint32(start64)
				chunkEnd = uint32(end64)
				useRange = true
			}
		} else {
			logs.Println("Expected two numbers; downloading full file instead.")
		}
	}
	if useRange {
		effectiveEnd := chunkEnd
		if effectiveEnd == 0 || effectiveEnd > totalChunks {
			effectiveEnd = totalChunks
		}
		if chunkStart >= effectiveEnd {
			logs.Printf("Invalid range [%d, %d); downloading full file instead.\n", chunkStart, chunkEnd)
			useRange = false
		} else {
			chunkEnd = effectiveEnd
		}
	}

	// Resolve output path
	outputPath := filepath.Join(cfg.KeyStore.StorageDir, filepath.Base(selectedMD.FileName))
	if useRange {
		base := strings.TrimSuffix(filepath.Base(selectedMD.FileName), filepath.Ext(selectedMD.FileName))
		ext := filepath.Ext(selectedMD.FileName)
		outputPath = filepath.Join(cfg.KeyStore.StorageDir,
			fmt.Sprintf("%s.chunks_%d_%d%s", base, chunkStart, chunkEnd, ext))
	}

	if err := createDirPath(filepath.Dir(outputPath)); err != nil {
		return fmt.Errorf("failed to ensure output directory: %w", err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer f.Close()

	showBar := !cfg.KeyStore.Verbose
	summary := OpSummary{
		Operation: "local-download",
		FileName:  selectedMD.FileName,
		FileSize:  selectedMD.TotalSize,
		Timer:     tui.NewPhaseTimer(),
		StartedAt: time.Now(),
	}

	// Estimate total bytes for progress bar
	var streamTotal uint64
	if useRange {
		streamTotal = uint64(chunkEnd-chunkStart) * uint64(selectedMD.BlockSize)
	} else {
		streamTotal = selectedMD.TotalSize
	}

	var pbOut io.Writer = os.Stderr
	if !showBar {
		pbOut = io.Discard
	}
	pb := tui.NewProgressBar(f, tui.ProgressBarParams{
		Label: "download",
		Total: int64(streamTotal),
		Out:   pbOut,
	})

	stageLabel := "download full file to output path"
	if useRange {
		stageLabel = "download selected chunk range to output path"
	}
	beginPhase(summary.Timer, summary.Operation, "download", stageLabel, 1, 1)
	if useRange {
		logs.Printf("\nDownloading chunks [%d, %d) of %q to %s\n", chunkStart, chunkEnd, selectedMD.FileName, outputPath)
		_, downloadErr := ks.StreamChunkRange(selectedMD.FileHash, chunkStart, chunkEnd, pb)
		pb.Done()
		summary.Timer.End()
		summary.Bytes = uint64(pb.Written())
		if downloadErr != nil {
			summary.Err = downloadErr
			renderSummary(t, summary)
			writeOpLog(summary)
			return fmt.Errorf("download failed: %w", downloadErr)
		}
		logs.Printf("Downloaded %s to %s\n", formatBytes(summary.Bytes), outputPath)
	} else {
		logs.Printf("\nDownloading %q to %s\n", selectedMD.FileName, outputPath)
		downloadErr := ks.StreamFile(selectedMD.FileHash, pb)
		pb.Done()
		summary.Timer.End()
		summary.Bytes = uint64(pb.Written())
		if downloadErr != nil {
			summary.Err = downloadErr
			renderSummary(t, summary)
			writeOpLog(summary)
			return fmt.Errorf("download failed: %w", downloadErr)
		}
		logs.Printf("Downloaded %s to %s\n", formatBytes(summary.Bytes), outputPath)
	}

	renderSummary(t, summary)
	writeOpLog(summary)
	return nil
}
