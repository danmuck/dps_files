package nodes

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/danmuck/dps_files/src/api/ledgers"
)

func (s *DefaultServerNode) registerHTTPRoutes() {
	s.mux.HandleFunc("PUT /files/{name}", s.handleUpload)
	s.mux.HandleFunc("GET /files/hash/{hex}", s.handleDownloadByHash)
	s.mux.HandleFunc("DELETE /files/hash/{hex}", s.handleDeleteByHash)
	s.mux.HandleFunc("GET /files/{name}", s.handleDownloadByName)
	s.mux.HandleFunc("GET /files", s.handleListFiles)
	s.mux.HandleFunc("GET /dirs/hash/{hex}", s.handleListDir)
	s.mux.HandleFunc("GET /dirs/hash/{hex}/tree", s.handleListDirTree)
}

func (s *DefaultServerNode) handleUpload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "missing file name", http.StatusBadRequest)
		return
	}
	clStr := r.Header.Get("Content-Length")
	if clStr == "" {
		http.Error(w, "Content-Length required", http.StatusBadRequest)
		return
	}
	size, err := strconv.ParseUint(clStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid Content-Length", http.StatusBadRequest)
		return
	}
	fid, err := s.storage.StoreFromReader(name, r.Body, size)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"hash": hex.EncodeToString(fid[:]),
		"size": size,
		"name": name,
	})
}

func (s *DefaultServerNode) handleDownloadByHash(w http.ResponseWriter, r *http.Request) {
	hexStr := r.PathValue("hex")
	hashBytes, err := hex.DecodeString(hexStr)
	if err != nil || len(hashBytes) != 32 {
		http.Error(w, "invalid hash", http.StatusBadRequest)
		return
	}
	var fid ledgers.FileID
	copy(fid[:], hashBytes)

	w.Header().Set("Content-Type", "application/octet-stream")
	if err := s.storage.StreamFile(fid, w); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
}

func (s *DefaultServerNode) handleDownloadByName(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	if err := s.storage.StreamFileByName(name, w); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
}

func (s *DefaultServerNode) handleDeleteByHash(w http.ResponseWriter, r *http.Request) {
	hexStr := r.PathValue("hex")
	hashBytes, err := hex.DecodeString(hexStr)
	if err != nil || len(hashBytes) != 32 {
		http.Error(w, "invalid hash", http.StatusBadRequest)
		return
	}
	var fid ledgers.FileID
	copy(fid[:], hashBytes)

	if err := s.storage.DeleteFile(fid); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *DefaultServerNode) handleListDir(w http.ResponseWriter, r *http.Request) {
	hexStr := r.PathValue("hex")
	hashBytes, err := hex.DecodeString(hexStr)
	if err != nil || len(hashBytes) != 32 {
		http.Error(w, "invalid hash", http.StatusBadRequest)
		return
	}
	var fid ledgers.FileID
	copy(fid[:], hashBytes)

	entries, err := s.storage.ListDirectory(fid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

func (s *DefaultServerNode) handleListDirTree(w http.ResponseWriter, r *http.Request) {
	hexStr := r.PathValue("hex")
	hashBytes, err := hex.DecodeString(hexStr)
	if err != nil || len(hashBytes) != 32 {
		http.Error(w, "invalid hash", http.StatusBadRequest)
		return
	}
	var fid ledgers.FileID
	copy(fid[:], hashBytes)

	tree, err := s.buildTree(fid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tree)
}

func (s *DefaultServerNode) buildTree(fid ledgers.FileID) ([]ledgers.DirectoryEntry, error) {
	entries, err := s.storage.ListDirectory(fid)
	if err != nil {
		return nil, err
	}
	var all []ledgers.DirectoryEntry
	for _, e := range entries {
		all = append(all, e)
		if e.Type == "directory" {
			sub, err := s.buildTree(e.Hash)
			if err != nil {
				return nil, err
			}
			all = append(all, sub...)
		}
	}
	return all, nil
}

func (s *DefaultServerNode) handleListFiles(w http.ResponseWriter, r *http.Request) {
	summaries := s.storage.ListKnownFilesMetadata()
	type entry struct {
		Name string `json:"name"`
		Hash string `json:"hash"`
		Size uint64 `json:"size"`
	}
	entries := make([]entry, len(summaries))
	for i, sm := range summaries {
		entries[i] = entry{
			Name: sm.Name,
			Hash: hex.EncodeToString(sm.Hash[:]),
			Size: sm.Size,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}
