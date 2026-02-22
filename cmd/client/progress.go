package main

import (
	"fmt"
	"os"
	"time"

	tui "github.com/danmuck/tui_go"
	logs "github.com/danmuck/smplog"
)

// OpSummary holds the result of an operation for display and logging.
type OpSummary struct {
	Operation string // "local-store", "remote-upload", "local-download", "remote-download"
	FileName  string
	FileSize  uint64
	Bytes     uint64 // bytes transferred
	Timer     *tui.PhaseTimer
	StartedAt time.Time
	Err       error
}

// beginPhase prints the current operation stage and starts timing that phase.
func beginPhase(timer *tui.PhaseTimer, operation, phaseName, stageLabel string, stageIndex, stageTotal int) {
	logs.Printf("\n[%s] Stage %d/%d: %s\n", operation, stageIndex, stageTotal, stageLabel)
	timer.Begin(phaseName)
}

// renderSummary prints an OperationSummary via tui_go.
func renderSummary(t tui.TUI, s OpSummary) {
	ok := s.Err == nil
	title := fmt.Sprintf("%s: %s", s.Operation, s.FileName)

	var fields []tui.SummaryField
	if s.FileSize > 0 {
		fields = append(fields, tui.SummaryField{Label: "total size", Value: formatBytes(s.FileSize)})
	}
	if s.Bytes > 0 {
		fields = append(fields, tui.SummaryField{Label: "bytes transferred", Value: formatBytes(s.Bytes)})
	}
	if !ok {
		fields = append(fields, tui.SummaryField{Label: "error", Value: s.Err.Error()})
	}
	elapsed := s.Timer.Elapsed()
	if s.Bytes > 0 && elapsed.Seconds() > 0 {
		throughput := float64(s.Bytes) / elapsed.Seconds()
		fields = append(fields, tui.SummaryField{Label: "avg throughput", Value: formatBytes(uint64(throughput)) + "/s"})
	}

	t.OperationSummaryTC(&tui.OperationSummaryParams{
		Title:  title,
		OK:     ok,
		Fields: fields,
		Timer:  s.Timer,
	})
}

// writeOpLog appends a plain-text log entry to ./local/logs/YYYY-MM-DD-{op}.log.
func writeOpLog(s OpSummary) {
	dir := "./local/logs"
	if err := createDirPath(dir); err != nil {
		logs.Warnf("could not create log dir: %v", err)
		return
	}
	date := s.StartedAt.Format("2006-01-02")
	path := fmt.Sprintf("%s/%s-%s.log", dir, date, s.Operation)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logs.Warnf("could not open log file %s: %v", path, err)
		return
	}
	defer f.Close()

	status := "OK"
	if s.Err != nil {
		status = fmt.Sprintf("FAILED: %v", s.Err)
	}

	totalElapsed := s.Timer.Elapsed()
	fmt.Fprintf(f, "[%s] op=%s file=%s size=%d bytes=%d status=%s\n",
		s.StartedAt.Format(time.RFC3339),
		s.Operation,
		s.FileName,
		s.FileSize,
		s.Bytes,
		status,
	)
	for _, ph := range s.Timer.Phases() {
		fmt.Fprintf(f, "  phase=%q elapsed=%s\n", ph.Label, ph.Elapsed)
	}
	fmt.Fprintf(f, "  total=%s\n\n", totalElapsed)
}
