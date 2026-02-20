package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/danmuck/dps_files/src/key_store"
)

func executeRemoteStreamAction(cfg RuntimeConfig, input io.Reader) error {
	client := NewFileServerClient(cfg.RemoteAddr)
	client.Timeout = 0 // no deadline for large downloads

	entries, err := client.List()
	if err != nil {
		return fmt.Errorf("list remote files: %w", err)
	}
	if len(entries) == 0 {
		fmt.Println("No files on remote server.")
		return nil
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	fmt.Printf("\nRemote files (%d):\n", len(entries))
	for i, e := range entries {
		shortHash := e.Hash
		if len(shortHash) > 16 {
			shortHash = shortHash[:16]
		}
		fmt.Printf("  [%d] %s  hash: %s...  size: %s\n", i, e.Name, shortHash, formatBytes(e.Size))
	}

	reader := getBufferedReader(input)
	var selected RemoteFileEntry
	for {
		fmt.Printf("\nSelect file to download [0-%d] (or e to cancel): ", len(entries)-1)
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
		if convErr != nil || idx < 0 || idx >= len(entries) {
			fmt.Printf("Invalid selection %q.\n", choice)
			continue
		}
		selected = entries[idx]
		break
	}

	outputPath := copyOutputPath(cfg.KeyStore.StorageDir, selected.Name)
	fmt.Printf("\nDownloading %q to %s\n", selected.Name, outputPath)

	showBar := !cfg.KeyStore.Verbose
	summary := OpSummary{
		Operation: "remote-download",
		FileName:  selected.Name,
		FileSize:  selected.Size,
		StartedAt: time.Now(),
	}

	pw := newProgressWriter(io.Discard, selected.Size, "download", showBar)
	summary.Timer.Start("download")
	written, downloadErr := client.Download(selected.Name, outputPath, pw)
	pw.Finish()
	summary.Timer.Stop(downloadErr != nil)

	summary.Bytes = written
	if downloadErr != nil {
		summary.Err = downloadErr
		renderSummary(summary)
		writeOpLog(summary)
		return fmt.Errorf("download %q: %w", selected.Name, downloadErr)
	}

	fmt.Printf("Downloaded %s to %s\n", formatBytes(written), outputPath)
	renderSummary(summary)
	writeOpLog(summary)
	return nil
}

func executeStreamAction(cfg RuntimeConfig, ks *key_store.KeyStore, input io.Reader) error {
	if cfg.Mode == ModeRemote {
		return executeRemoteStreamAction(cfg, input)
	}
	metadata := ks.ListKnownFiles()
	if len(metadata) == 0 {
		fmt.Println("No stored files to stream.")
		return nil
	}

	sort.Slice(metadata, func(i, j int) bool {
		if metadata[i].FileName == metadata[j].FileName {
			return fmt.Sprintf("%x", metadata[i].FileHash) < fmt.Sprintf("%x", metadata[j].FileHash)
		}
		return metadata[i].FileName < metadata[j].FileName
	})

	fmt.Printf("\nStored files (%d):\n", len(metadata))
	for i, md := range metadata {
		hashHex := fmt.Sprintf("%x", md.FileHash)
		shortHash := hashHex
		if len(shortHash) > 16 {
			shortHash = shortHash[:16]
		}
		fmt.Printf("  [%d] %s  hash: %s...  chunks: %d  size: %s\n",
			i, md.FileName, shortHash, md.TotalBlocks, formatBytes(md.TotalSize))
	}

	reader := getBufferedReader(input)

	// Select file
	var selectedMD key_store.MetaData
	for {
		fmt.Printf("\nSelect file to stream [0-%d] (or e to cancel): ", len(metadata)-1)
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
			fmt.Printf("Invalid selection %q. Enter a numeric index or e.\n", choice)
			continue
		}
		if idx < 0 || idx >= len(metadata) {
			fmt.Printf("Index %d out of range. Valid range is 0-%d.\n", idx, len(metadata)-1)
			continue
		}

		selectedMD = metadata[idx]
		break
	}

	// Optional chunk range
	totalChunks := selectedMD.TotalBlocks
	var chunkStart, chunkEnd uint32
	useRange := false

	fmt.Printf("\nChunk range (total: %d chunks). Enter start end (e.g. '0 10') or press Enter for full file: ", totalChunks)
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
				fmt.Println("Invalid range; streaming full file instead.")
			} else {
				chunkStart = uint32(start64)
				chunkEnd = uint32(end64)
				useRange = true
			}
		} else {
			fmt.Println("Expected two numbers; streaming full file instead.")
		}
	}
	if useRange {
		effectiveEnd := chunkEnd
		if effectiveEnd == 0 || effectiveEnd > totalChunks {
			effectiveEnd = totalChunks
		}
		if chunkStart >= effectiveEnd {
			fmt.Printf("Invalid range [%d, %d); streaming full file instead.\n", chunkStart, chunkEnd)
			useRange = false
		} else {
			chunkEnd = effectiveEnd
		}
	}

	// Resolve output path
	outputPath := copyOutputPath(cfg.KeyStore.StorageDir, selectedMD.FileName)
	if useRange {
		base := strings.TrimSuffix(filepath.Base(selectedMD.FileName), filepath.Ext(selectedMD.FileName))
		ext := filepath.Ext(selectedMD.FileName)
		outputPath = filepath.Join(cfg.KeyStore.StorageDir,
			fmt.Sprintf("copy.%s.chunks_%d_%d%s", base, chunkStart, chunkEnd, ext))
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
		Operation: "local-stream",
		FileName:  selectedMD.FileName,
		FileSize:  selectedMD.TotalSize,
		StartedAt: time.Now(),
	}

	// Estimate total bytes for progress bar
	var streamTotal uint64
	if useRange {
		streamTotal = uint64(chunkEnd-chunkStart) * uint64(selectedMD.BlockSize)
	} else {
		streamTotal = selectedMD.TotalSize
	}

	pw := newProgressWriter(f, streamTotal, "stream", showBar)

	summary.Timer.Start("stream")
	if useRange {
		fmt.Printf("\nStreaming chunks [%d, %d) of %q to %s\n", chunkStart, chunkEnd, selectedMD.FileName, outputPath)
		_, streamErr := ks.StreamChunkRange(selectedMD.FileHash, chunkStart, chunkEnd, pw)
		pw.Finish()
		summary.Timer.Stop(streamErr != nil)
		summary.Bytes = pw.Written()
		if streamErr != nil {
			summary.Err = streamErr
			renderSummary(summary)
			writeOpLog(summary)
			return fmt.Errorf("stream failed: %w", streamErr)
		}
		fmt.Printf("Streamed %s to %s\n", formatBytes(summary.Bytes), outputPath)
	} else {
		fmt.Printf("\nStreaming %q to %s\n", selectedMD.FileName, outputPath)
		streamErr := ks.StreamFile(selectedMD.FileHash, pw)
		pw.Finish()
		summary.Timer.Stop(streamErr != nil)
		summary.Bytes = pw.Written()
		if streamErr != nil {
			summary.Err = streamErr
			renderSummary(summary)
			writeOpLog(summary)
			return fmt.Errorf("stream failed: %w", streamErr)
		}
		fmt.Printf("Streamed %s to %s\n", formatBytes(summary.Bytes), outputPath)
	}

	renderSummary(summary)
	writeOpLog(summary)
	return nil
}
