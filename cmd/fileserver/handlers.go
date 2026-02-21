package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"

	"github.com/danmuck/dps_files/src/key_store"
	logs "github.com/danmuck/smplog"
)

func handleConn(ks *key_store.KeyStore, conn net.Conn) {
	defer conn.Close()

	// Read the command frame
	frame, err := readFrame(conn)
	if err != nil {
		logs.Warnf("read frame: %v", err)
		return
	}
	if len(frame) < 1 {
		writeError(conn, "empty frame")
		return
	}

	cmd := frame[0]
	payload := frame[1:]

	switch cmd {
	case CmdUpload:
		handleUpload(ks, conn, payload)
	case CmdDownload:
		handleDownload(ks, conn, payload)
	case CmdList:
		handleList(ks, conn)
	case CmdDelete:
		handleDelete(ks, conn, payload)
	default:
		writeError(conn, fmt.Sprintf("unknown command: 0x%02x", cmd))
	}
}

// UPLOAD payload: [2B name_len][name][8B file_size][file data...]
// The file data is read directly from the connection after the frame.
//
// For simplicity in the frame-based protocol, the upload command frame contains
// the name and size header. The actual file bytes follow as raw data on the
// connection (not framed), which allows streaming without buffering.
func handleUpload(ks *key_store.KeyStore, conn net.Conn, header []byte) {
	if len(header) < 10 { // 2 + 8 minimum
		writeError(conn, "upload header too short")
		return
	}

	nameLen := binary.BigEndian.Uint16(header[0:2])
	if int(nameLen) > len(header)-10 {
		writeError(conn, "invalid name length")
		return
	}
	name := string(header[2 : 2+nameLen])
	fileSize := binary.BigEndian.Uint64(header[2+nameLen : 10+nameLen])

	// Remaining bytes in the header frame are the start of file data
	remaining := header[10+nameLen:]

	// Build a reader: first the remaining header bytes, then the raw connection
	var dataReader io.Reader
	bytesInHeader := uint64(len(remaining))
	if bytesInHeader >= fileSize {
		// All data was in the frame
		dataReader = io.LimitReader(
			io.MultiReader(
				bytesReader(remaining),
				conn,
			), int64(fileSize))
	} else {
		dataReader = io.MultiReader(
			bytesReader(remaining),
			io.LimitReader(conn, int64(fileSize-bytesInHeader)),
		)
	}

	file, err := ks.StoreFromReader(name, dataReader, fileSize)
	if err != nil {
		writeError(conn, err.Error())
		return
	}

	// Response: [1B status][32B file_hash]
	resp := make([]byte, 1+key_store.HashSize)
	resp[0] = StatusOK
	copy(resp[1:], file.MetaData.FileHash[:])
	conn.Write(resp)
}

// bytesReader wraps a byte slice as an io.Reader.
func bytesReader(b []byte) io.Reader {
	return io.LimitReader(
		readerFunc(func(p []byte) (int, error) {
			if len(b) == 0 {
				return 0, io.EOF
			}
			n := copy(p, b)
			b = b[n:]
			return n, nil
		}),
		int64(len(b)),
	)
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// DOWNLOAD payload: [1B type: 0=hash, 1=name][key_or_name]
func handleDownload(ks *key_store.KeyStore, conn net.Conn, payload []byte) {
	if len(payload) < 2 {
		writeError(conn, "download payload too short")
		return
	}

	lookupType := payload[0]
	key := payload[1:]

	var file *key_store.File
	var err error

	switch lookupType {
	case 0: // by hash
		if len(key) != key_store.HashSize {
			writeError(conn, "invalid hash length")
			return
		}
		var hash [key_store.HashSize]byte
		copy(hash[:], key)
		file, err = ks.GetFileByHash(hash)
	case 1: // by name
		file, err = ks.GetFileByName(string(key))
	default:
		writeError(conn, fmt.Sprintf("invalid lookup type: %d", lookupType))
		return
	}

	if err != nil {
		resp := []byte{StatusNotFound}
		conn.Write(resp)
		return
	}

	// Response: [1B status][8B size] then raw byte stream
	resp := make([]byte, 9)
	resp[0] = StatusOK
	binary.BigEndian.PutUint64(resp[1:], file.MetaData.TotalSize)
	if _, err := conn.Write(resp); err != nil {
		return
	}

	// Stream file data directly to connection
	if err := ks.StreamFile(file.MetaData.FileHash, conn); err != nil {
		logs.Warnf("stream error: %v", err)
	}
}

func handleList(ks *key_store.KeyStore, conn net.Conn) {
	files := ks.ListKnownFiles()
	type fileEntry struct {
		Name string `json:"name"`
		Hash string `json:"hash"`
		Size uint64 `json:"size"`
	}
	entries := make([]fileEntry, len(files))
	for i, f := range files {
		entries[i] = fileEntry{
			Name: f.FileName,
			Hash: hex.EncodeToString(f.FileHash[:]),
			Size: f.TotalSize,
		}
	}
	writeJSON(conn, entries)
}

// DELETE payload: [32B file_hash]
func handleDelete(ks *key_store.KeyStore, conn net.Conn, payload []byte) {
	if len(payload) != key_store.HashSize {
		writeError(conn, "invalid hash length")
		return
	}
	var hash [key_store.HashSize]byte
	copy(hash[:], payload)

	if err := ks.DeleteFile(hash); err != nil {
		writeError(conn, err.Error())
		return
	}
	writeStatus(conn, StatusOK)
}
