package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// Command bytes
const (
	CmdUpload   byte = 0x01
	CmdDownload byte = 0x02
	CmdList     byte = 0x03
	CmdDelete   byte = 0x04
)

// Status bytes
const (
	StatusOK       byte = 0x00
	StatusNotFound byte = 0x01
	StatusError    byte = 0x02
)

// readFrame reads a 4-byte big-endian length prefix followed by the payload.
func readFrame(r io.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("failed to read frame length: %w", err)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("failed to read frame body: %w", err)
	}
	return buf, nil
}

// writeFrame writes a 4-byte big-endian length prefix followed by the payload.
func writeFrame(w io.Writer, data []byte) error {
	if err := binary.Write(w, binary.BigEndian, uint32(len(data))); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

// writeStatus writes a single status byte.
func writeStatus(w io.Writer, status byte) error {
	_, err := w.Write([]byte{status})
	return err
}

// writeError writes StatusError followed by the error string as a frame.
func writeError(w io.Writer, msg string) error {
	if err := writeStatus(w, StatusError); err != nil {
		return err
	}
	return writeFrame(w, []byte(msg))
}

// writeJSON writes StatusOK followed by JSON-encoded data as a frame.
func writeJSON(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := writeStatus(w, StatusOK); err != nil {
		return err
	}
	return writeFrame(w, data)
}
