package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/danmuck/dps_files/src/key_store"
)

type fileResponse struct {
	Hash string `json:"hash"`
	Size uint64 `json:"size"`
	Name string `json:"name"`
}

func handleUpload(ks *key_store.KeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			http.Error(w, "missing filename", http.StatusBadRequest)
			return
		}

		size, err := strconv.ParseUint(r.Header.Get("Content-Length"), 10, 64)
		if err != nil || size == 0 {
			http.Error(w, "Content-Length required", http.StatusBadRequest)
			return
		}

		file, err := ks.StoreFromReader(name, r.Body, size)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(fileResponse{
			Hash: hex.EncodeToString(file.MetaData.FileHash[:]),
			Size: file.MetaData.TotalSize,
			Name: file.MetaData.FileName,
		})
	}
}

func handleDownloadByName(ks *key_store.KeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		file, err := ks.GetFileByName(name)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		serveFile(ks, w, r, file)
	}
}

func handleDownloadByHash(ks *key_store.KeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hexHash := r.PathValue("hex")
		hashBytes, err := hex.DecodeString(hexHash)
		if err != nil || len(hashBytes) != key_store.HashSize {
			http.Error(w, "invalid hash", http.StatusBadRequest)
			return
		}
		var hash [key_store.HashSize]byte
		copy(hash[:], hashBytes)

		file, err := ks.GetFileByHash(hash)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		serveFile(ks, w, r, file)
	}
}

func serveFile(ks *key_store.KeyStore, w http.ResponseWriter, r *http.Request, file *key_store.File) {
	totalSize := file.MetaData.TotalSize
	blockSize := uint64(file.MetaData.BlockSize)

	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", file.MetaData.FileName))

	// Check for Range header
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" || blockSize == 0 {
		// Full file download
		w.Header().Set("Content-Length", strconv.FormatUint(totalSize, 10))
		w.Header().Set("Content-Type", "application/octet-stream")
		if err := ks.StreamFile(file.MetaData.FileHash, w); err != nil {
			// Headers already sent, can't change status
			return
		}
		return
	}

	// Parse Range: bytes=START-END
	start, end, ok := parseRange(rangeHeader, totalSize)
	if !ok {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", totalSize))
		http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	contentLen := end - start + 1
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatUint(contentLen, 10))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, totalSize))
	w.WriteHeader(http.StatusPartialContent)

	// Calculate chunk range
	startChunk := uint32(start / blockSize)
	endChunk := uint32(end/blockSize) + 1
	skipBytes := start % blockSize

	// Stream the chunk range through a byte-trimming writer
	tw := &trimWriter{
		w:     w,
		skip:  int64(skipBytes),
		limit: int64(contentLen),
	}
	ks.StreamChunkRange(file.MetaData.FileHash, startChunk, endChunk, tw)
}

// parseRange parses "bytes=START-END" and returns inclusive byte offsets.
func parseRange(header string, totalSize uint64) (uint64, uint64, bool) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(header, "bytes=")
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}

	var start, end uint64
	var err error

	if parts[0] == "" {
		// Suffix range: bytes=-N (last N bytes)
		suffix, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil || suffix == 0 {
			return 0, 0, false
		}
		if suffix > totalSize {
			suffix = totalSize
		}
		start = totalSize - suffix
		end = totalSize - 1
	} else {
		start, err = strconv.ParseUint(parts[0], 10, 64)
		if err != nil || start >= totalSize {
			return 0, 0, false
		}
		if parts[1] == "" {
			end = totalSize - 1
		} else {
			end, err = strconv.ParseUint(parts[1], 10, 64)
			if err != nil {
				return 0, 0, false
			}
			if end >= totalSize {
				end = totalSize - 1
			}
		}
	}

	if start > end {
		return 0, 0, false
	}
	return start, end, true
}

// trimWriter skips the first `skip` bytes and stops after `limit` bytes.
type trimWriter struct {
	w       io.Writer
	skip    int64
	limit   int64
	written int64
}

func (tw *trimWriter) Write(p []byte) (int, error) {
	consumed := len(p) // report all bytes as consumed to caller

	// Skip leading bytes
	if tw.skip > 0 {
		if int64(len(p)) <= tw.skip {
			tw.skip -= int64(len(p))
			return consumed, nil
		}
		p = p[tw.skip:]
		tw.skip = 0
	}

	// Enforce limit
	remaining := tw.limit - tw.written
	if remaining <= 0 {
		return consumed, nil
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}

	n, err := tw.w.Write(p)
	tw.written += int64(n)
	if err != nil {
		return n, err
	}
	return consumed, nil
}

func handleListFiles(ks *key_store.KeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		files := ks.ListKnownFiles()
		entries := make([]fileResponse, len(files))
		for i, f := range files {
			entries[i] = fileResponse{
				Hash: hex.EncodeToString(f.FileHash[:]),
				Size: f.TotalSize,
				Name: f.FileName,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	}
}

func handleDeleteByHash(ks *key_store.KeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hexHash := r.PathValue("hex")
		hashBytes, err := hex.DecodeString(hexHash)
		if err != nil || len(hashBytes) != key_store.HashSize {
			http.Error(w, "invalid hash", http.StatusBadRequest)
			return
		}
		var hash [key_store.HashSize]byte
		copy(hash[:], hashBytes)

		if err := ks.DeleteFile(hash); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
