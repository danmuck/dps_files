package main

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

// RemoteFileEntry is a file entry returned by the fileserver List command.
type RemoteFileEntry struct {
	Name string `json:"name"`
	Hash string `json:"hash"` // hex-encoded 32-byte SHA-256
	Size uint64 `json:"size"`
}

// FileServerClient dials cmd/fileserver over TCP.
type FileServerClient struct {
	Addr    string
	Timeout time.Duration // 0 = no deadline (use for large transfers)
}

// NewFileServerClient returns a client with a 30-second default timeout.
func NewFileServerClient(addr string) *FileServerClient {
	return &FileServerClient{Addr: addr, Timeout: 30 * time.Second}
}

func (c *FileServerClient) dial() (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", c.Addr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", c.Addr, err)
	}
	if c.Timeout > 0 {
		if err := conn.SetDeadline(time.Now().Add(c.Timeout)); err != nil {
			conn.Close()
			return nil, fmt.Errorf("set deadline: %w", err)
		}
	}
	return conn, nil
}

// remoteReadFrame reads a 4-byte big-endian length prefix then the payload.
func remoteReadFrame(r io.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("read frame length: %w", err)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("read frame body: %w", err)
	}
	return buf, nil
}

// remoteWriteFrame writes a 4-byte big-endian length prefix then the payload.
func remoteWriteFrame(w io.Writer, data []byte) error {
	if err := binary.Write(w, binary.BigEndian, uint32(len(data))); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

// readErrorFrame reads the error message frame that follows a StatusError byte.
func readErrorFrame(conn net.Conn) string {
	msg, err := remoteReadFrame(conn)
	if err != nil {
		return "(could not read server error message)"
	}
	return string(msg)
}

// Upload sends localPath to the fileserver and returns the server-assigned SHA-256 hash.
// Use Timeout=0 for large files so no deadline fires mid-transfer.
func (c *FileServerClient) Upload(localPath string) ([32]byte, error) {
	var hash [32]byte

	f, err := os.Open(localPath)
	if err != nil {
		return hash, fmt.Errorf("open %s: %w", localPath, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return hash, fmt.Errorf("stat %s: %w", localPath, err)
	}
	fileSize := uint64(info.Size())
	name := filepath.Base(localPath)
	nameBytes := []byte(name)

	// Frame body: [0x01][2B name_len][name][8B file_size]
	frame := make([]byte, 1+2+len(nameBytes)+8)
	frame[0] = 0x01 // CmdUpload
	binary.BigEndian.PutUint16(frame[1:3], uint16(len(nameBytes)))
	copy(frame[3:3+len(nameBytes)], nameBytes)
	binary.BigEndian.PutUint64(frame[3+len(nameBytes):], fileSize)

	conn, err := c.dial()
	if err != nil {
		return hash, err
	}
	defer conn.Close()

	if err := remoteWriteFrame(conn, frame); err != nil {
		return hash, fmt.Errorf("write upload header frame: %w", err)
	}

	// Stream file data raw (not framed) after the header frame.
	if _, err := io.Copy(conn, f); err != nil {
		return hash, fmt.Errorf("stream file data: %w", err)
	}

	// Response: [1B status][32B hash]  — or [1B 0x02][frame: error msg]
	var statusBuf [1]byte
	if _, err := io.ReadFull(conn, statusBuf[:]); err != nil {
		return hash, fmt.Errorf("read upload response: %w", err)
	}
	switch statusBuf[0] {
	case 0x00: // StatusOK
	case 0x02: // StatusError
		return hash, fmt.Errorf("server error: %s", readErrorFrame(conn))
	default:
		return hash, fmt.Errorf("unexpected upload status 0x%02x", statusBuf[0])
	}

	if _, err := io.ReadFull(conn, hash[:]); err != nil {
		return hash, fmt.Errorf("read upload hash: %w", err)
	}
	return hash, nil
}

// List returns all files known to the fileserver.
func (c *FileServerClient) List() ([]RemoteFileEntry, error) {
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Frame body: [0x03]
	if err := remoteWriteFrame(conn, []byte{0x03}); err != nil {
		return nil, fmt.Errorf("write list command: %w", err)
	}

	// Response: [1B status] then frame with JSON
	var statusBuf [1]byte
	if _, err := io.ReadFull(conn, statusBuf[:]); err != nil {
		return nil, fmt.Errorf("read list status: %w", err)
	}
	switch statusBuf[0] {
	case 0x00: // StatusOK
	case 0x02:
		return nil, fmt.Errorf("server error: %s", readErrorFrame(conn))
	default:
		return nil, fmt.Errorf("unexpected list status 0x%02x", statusBuf[0])
	}

	data, err := remoteReadFrame(conn)
	if err != nil {
		return nil, fmt.Errorf("read list response frame: %w", err)
	}

	var entries []RemoteFileEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("decode list JSON: %w", err)
	}
	return entries, nil
}

// Download fetches a file by name from the fileserver and writes it to outputPath.
// Returns the number of bytes written.
func (c *FileServerClient) Download(name, outputPath string) (uint64, error) {
	conn, err := c.dial()
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	// Frame body: [0x02][0x01 (by-name)][name bytes]
	payload := make([]byte, 2+len(name))
	payload[0] = 0x02 // CmdDownload
	payload[1] = 0x01 // lookup by name
	copy(payload[2:], []byte(name))

	if err := remoteWriteFrame(conn, payload); err != nil {
		return 0, fmt.Errorf("write download command: %w", err)
	}

	// Response: [1B status][8B file_size] then raw stream
	var respHeader [9]byte
	if _, err := io.ReadFull(conn, respHeader[:]); err != nil {
		return 0, fmt.Errorf("read download header: %w", err)
	}
	switch respHeader[0] {
	case 0x00: // StatusOK
	case 0x01: // StatusNotFound — no error frame follows
		return 0, fmt.Errorf("file %q not found on server", name)
	default:
		return 0, fmt.Errorf("unexpected download status 0x%02x", respHeader[0])
	}

	fileSize := binary.BigEndian.Uint64(respHeader[1:9])

	if err := createDirPath(filepath.Dir(outputPath)); err != nil {
		return 0, fmt.Errorf("ensure output dir: %w", err)
	}
	outF, err := os.Create(outputPath)
	if err != nil {
		return 0, fmt.Errorf("create output file: %w", err)
	}
	defer outF.Close()

	written, err := io.Copy(outF, io.LimitReader(conn, int64(fileSize)))
	if err != nil {
		return 0, fmt.Errorf("download stream: %w", err)
	}
	return uint64(written), nil
}

// Delete removes the file identified by its 32-byte SHA-256 hash from the fileserver.
func (c *FileServerClient) Delete(hash [32]byte) error {
	conn, err := c.dial()
	if err != nil {
		return err
	}
	defer conn.Close()

	// Frame body: [0x04][32B hash]
	payload := make([]byte, 1+32)
	payload[0] = 0x04 // CmdDelete
	copy(payload[1:], hash[:])

	if err := remoteWriteFrame(conn, payload); err != nil {
		return fmt.Errorf("write delete command: %w", err)
	}

	// Response: [1B status] or [1B 0x02][frame: error msg]
	var statusBuf [1]byte
	if _, err := io.ReadFull(conn, statusBuf[:]); err != nil {
		return fmt.Errorf("read delete response: %w", err)
	}
	switch statusBuf[0] {
	case 0x00: // StatusOK
		return nil
	case 0x02:
		return fmt.Errorf("server error: %s", readErrorFrame(conn))
	default:
		return fmt.Errorf("unexpected delete status 0x%02x", statusBuf[0])
	}
}

// hexToHash decodes a 64-char hex string into a [32]byte.
func hexToHash(s string) ([32]byte, error) {
	var h [32]byte
	b, err := hex.DecodeString(s)
	if err != nil {
		return h, fmt.Errorf("invalid hex: %w", err)
	}
	if len(b) != 32 {
		return h, fmt.Errorf("expected 32 bytes, got %d", len(b))
	}
	copy(h[:], b)
	return h, nil
}
