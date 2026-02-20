package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// PhaseRecord holds the name and elapsed time of a completed phase.
type PhaseRecord struct {
	Name    string
	Elapsed time.Duration
	Err     bool
}

// PhaseTimer records per-phase timing for an operation.
type PhaseTimer struct {
	phases    []PhaseRecord
	current   string
	startedAt time.Time
}

func (pt *PhaseTimer) Start(name string) {
	pt.current = name
	pt.startedAt = time.Now()
}

func (pt *PhaseTimer) Stop(errored bool) {
	if pt.current == "" {
		return
	}
	pt.phases = append(pt.phases, PhaseRecord{
		Name:    pt.current,
		Elapsed: time.Since(pt.startedAt),
		Err:     errored,
	})
	pt.current = ""
}

func (pt *PhaseTimer) TotalElapsed() time.Duration {
	var elapsed time.Duration
	for _, phase := range pt.phases {
		elapsed += phase.Elapsed
	}
	if pt.current != "" {
		elapsed += time.Since(pt.startedAt)
	}
	return elapsed
}

func (pt *PhaseTimer) Phases() []PhaseRecord {
	return pt.phases
}

// beginPhase prints the current operation stage and starts timing that phase.
func beginPhase(timer *PhaseTimer, operation, phaseName, stageLabel string, stageIndex, stageTotal int) {
	fmt.Printf("\n[%s] Stage %d/%d: %s\n", operation, stageIndex, stageTotal, stageLabel)
	timer.Start(phaseName)
}

// OpSummary holds the result of an operation for display and logging.
type OpSummary struct {
	Operation string // "local-store", "remote-upload", "local-download", "remote-download"
	FileName  string
	FileSize  uint64
	Bytes     uint64 // bytes transferred
	Timer     PhaseTimer
	StartedAt time.Time
	Err       error
}

// progressWriter wraps an io.Writer, counts bytes, and renders an ANSI bar to stderr.
type progressWriter struct {
	dst      io.Writer
	total    uint64
	written  uint64 // accessed via atomic
	label    string
	showBar  bool
	lastDraw time.Time
	started  time.Time
}

func newProgressWriter(dst io.Writer, total uint64, label string, showBar bool) *progressWriter {
	return &progressWriter{
		dst:     dst,
		total:   total,
		label:   label,
		showBar: showBar,
		started: time.Now(),
	}
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.dst.Write(p)
	if n > 0 {
		atomic.AddUint64(&pw.written, uint64(n))
		pw.maybeRender()
	}
	return n, err
}

func (pw *progressWriter) Written() uint64 {
	return atomic.LoadUint64(&pw.written)
}

func (pw *progressWriter) maybeRender() {
	if !pw.showBar {
		return
	}
	if time.Since(pw.lastDraw) < 50*time.Millisecond {
		return
	}
	pw.render()
	pw.lastDraw = time.Now()
}

func (pw *progressWriter) render() {
	w := atomic.LoadUint64(&pw.written)
	elapsed := time.Since(pw.started)
	rate := float64(0)
	if elapsed.Seconds() > 0 {
		rate = float64(w) / elapsed.Seconds()
	}

	var pct float64
	if pw.total > 0 {
		pct = float64(w) / float64(pw.total) * 100
		if pct > 100 {
			pct = 100
		}
	}

	barWidth := 30
	filled := 0
	if pw.total > 0 {
		filled = int(float64(barWidth) * float64(w) / float64(pw.total))
		if filled > barWidth {
			filled = barWidth
		}
	}
	bar := strings.Repeat("=", filled) + strings.Repeat("-", barWidth-filled)

	label := pw.label
	if len(label) > 8 {
		label = label[:8]
	}

	fmt.Fprintf(os.Stderr, "\r  %-8s  [%s]  %5.1f%%  %s / %s  %s/s",
		label,
		bar,
		pct,
		formatBytes(w),
		formatBytes(pw.total),
		formatBytes(uint64(rate)),
	)
}

func (pw *progressWriter) Finish() {
	if pw.showBar {
		pw.render()
		fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", 80))
	}
}

// progressReader wraps an io.Reader, counts bytes, and renders an ANSI bar to stderr.
type progressReader struct {
	src io.Reader
	pw  *progressWriter
}

func newProgressReader(src io.Reader, total uint64, label string, showBar bool) *progressReader {
	pw := newProgressWriter(io.Discard, total, label, showBar)
	return &progressReader{src: src, pw: pw}
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.src.Read(p)
	if n > 0 {
		atomic.AddUint64(&pr.pw.written, uint64(n))
		pr.pw.maybeRender()
	}
	return n, err
}

func (pr *progressReader) BytesRead() uint64 {
	return pr.pw.Written()
}

func (pr *progressReader) Finish() {
	pr.pw.Finish()
}

// renderSummary prints a timing table to stdout after every operation.
func renderSummary(s OpSummary) {
	status := "OK"
	if s.Err != nil {
		status = fmt.Sprintf("FAILED: %v", s.Err)
	}
	totalElapsed := s.Timer.TotalElapsed()

	fmt.Printf("\n--- %s summary: %s [%s] ---\n", s.Operation, s.FileName, status)
	if s.FileSize > 0 {
		fmt.Printf("  %-20s %s\n", "total size", formatBytes(s.FileSize))
	}
	if s.Bytes > 0 {
		fmt.Printf("  %-20s %s\n", "bytes transferred", formatBytes(s.Bytes))
	}
	for _, ph := range s.Timer.Phases() {
		fmt.Printf("  %-20s %s\n", ph.Name, formatDuration(ph.Elapsed))
	}
	fmt.Printf("  %-20s %s\n", "total", formatDuration(totalElapsed))
	if s.Bytes > 0 && totalElapsed.Seconds() > 0 {
		throughput := float64(s.Bytes) / totalElapsed.Seconds()
		fmt.Printf("  %-20s %s/s\n", "avg throughput", formatBytes(uint64(throughput)))
	}
}

// writeOpLog appends a plain-text log entry to ./local/logs/YYYY-MM-DD-{op}.log.
func writeOpLog(s OpSummary) {
	dir := "./local/logs"
	if err := createDirPath(dir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create log dir: %v\n", err)
		return
	}
	date := s.StartedAt.Format("2006-01-02")
	path := fmt.Sprintf("%s/%s-%s.log", dir, date, s.Operation)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not open log file %s: %v\n", path, err)
		return
	}
	defer f.Close()

	status := "OK"
	if s.Err != nil {
		status = fmt.Sprintf("FAILED: %v", s.Err)
	}

	totalElapsed := s.Timer.TotalElapsed()
	fmt.Fprintf(f, "[%s] op=%s file=%s size=%d bytes=%d status=%s\n",
		s.StartedAt.Format(time.RFC3339),
		s.Operation,
		s.FileName,
		s.FileSize,
		s.Bytes,
		status,
	)
	for _, ph := range s.Timer.Phases() {
		fmt.Fprintf(f, "  phase=%q elapsed=%s\n", ph.Name, formatDuration(ph.Elapsed))
	}
	fmt.Fprintf(f, "  total=%s\n\n", formatDuration(totalElapsed))
}

// formatDuration formats a duration for summary display.
func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dus", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.3fs", d.Seconds())
}
