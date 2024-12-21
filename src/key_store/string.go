package key_store

import (
	"fmt"
	"strings"
	"time"
)

// string returns a formatted string representation of file
func (f *File) String() string {
	var b strings.Builder
	b.WriteString("File {\n")
	b.WriteString(fmt.Sprintf("  MetaData: %s\n", indent(f.MetaData.String())))
	b.WriteString("  References: [\n")
	for i, ref := range f.References {
		if ref != nil {
			b.WriteString(fmt.Sprintf("    %d: %s\n", i, indent(ref.String())))
		}
	}
	b.WriteString("  ]\n")
	b.WriteString("}")
	return b.String()
}

// string returns a formatted string representation of filereference
func (fr *FileReference) String() string {
	var b strings.Builder
	b.WriteString("FileReference {\n")
	b.WriteString(fmt.Sprintf("  Key: %x\n", fr.Key))
	b.WriteString(fmt.Sprintf("  FileName: %s\n", fr.FileName))
	b.WriteString(fmt.Sprintf("  Parent: %x\n", fr.Parent))
	b.WriteString(fmt.Sprintf("  ChunkSize: %d\n", fr.Size))
	b.WriteString(fmt.Sprintf("  ChunkIndex: %d\n", fr.FileIndex))
	b.WriteString(fmt.Sprintf("  Location: %s\n", fr.Location))
	b.WriteString(fmt.Sprintf("  Protocol: %s\n", fr.Protocol))
	b.WriteString(fmt.Sprintf("  DataHash: %x\n", fr.DataHash))
	b.WriteString("}")
	return b.String()
}

// string returns a formatted string representation of metadata
func (md *MetaData) String() string {
	var b strings.Builder
	b.WriteString("MetaData {\n")
	b.WriteString(fmt.Sprintf("  FileHash: %x\n", md.FileHash))
	b.WriteString(fmt.Sprintf("  TotalSize: %d\n", md.TotalSize))
	b.WriteString(fmt.Sprintf("  FileName: %s\n", md.FileName))
	b.WriteString(fmt.Sprintf("  Modified: %s\n", time.Unix(0, md.Modified).Format(time.RFC3339)))
	// b.WriteString(fmt.Sprintf("  MimeType: %s\n", md.MimeType))
	b.WriteString(fmt.Sprintf("  Permissions: %o\n", md.Permissions))
	b.WriteString(fmt.Sprintf("  Signature: %x\n", md.Signature))
	b.WriteString(fmt.Sprintf("  TTL: %d\n", md.TTL))
	b.WriteString(fmt.Sprintf("  ChunkSize: %d\n", md.BlockSize))
	b.WriteString(fmt.Sprintf("  TotalChunks: %d\n", md.TotalBlocks))
	b.WriteString("}")
	return b.String()
}

// helper function to indent multiline strings
func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = "  " + line
		}
	}
	return strings.Join(lines, "\n")
}

// shortstring methods for more concise output when needed
func (f *File) ShortString() string {
	return fmt.Sprintf("File{name: %s, size: %d, chunks: %d}",
		f.MetaData.FileName, f.MetaData.TotalSize, f.MetaData.TotalBlocks)
}

func (fr *FileReference) ShortString() string {
	return fmt.Sprintf("FileRef{idx: %d, size: %d, hash: %x...}",
		fr.FileIndex, fr.Size, fr.DataHash[:4])
}

func (md *MetaData) ShortString() string {
	return fmt.Sprintf("MetaData{name: %s, size: %d, chunks: %d}",
		md.FileName, md.TotalSize, md.TotalBlocks)
}

// helper function to format byte sizes
func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
