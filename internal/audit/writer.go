package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Writer appends hash-chained records to a JSONL audit file. It is safe
// for concurrent use - a mutex serializes Append calls so the hash chain
// remains valid regardless of goroutine scheduling order.
type Writer struct {
	mu       sync.Mutex
	f        *os.File
	w        *bufio.Writer
	actor    Actor
	prevHash string
	seq      int64
	now      func() time.Time
}

// OpenWriter opens (creating if needed) the JSONL audit file at path and
// scans its tail to recover the next sequence number and previous hash.
// Pass a custom now function in tests to get stable timestamps; pass nil
// for time.Now.
func OpenWriter(path string, actor Actor, now func() time.Time) (*Writer, error) {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("audit: mkdir: %w", err)
	}
	prev, nextSeq, err := tailState(path)
	if err != nil {
		return nil, fmt.Errorf("audit: tail state: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("audit: open: %w", err)
	}
	return &Writer{
		f:        f,
		w:        bufio.NewWriter(f),
		actor:    actor,
		prevHash: prev,
		seq:      nextSeq,
		now:      now,
	}, nil
}

// Append writes one record to the log. It computes the record hash,
// updates the chain state, and flushes the buffer so partial runs
// still produce a durable trail.
func (w *Writer) Append(command string, req Request, resp Response) (Record, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	r := Record{
		Seq:      w.seq,
		TS:       w.now().UTC().Format(time.RFC3339Nano),
		Actor:    w.actor,
		Command:  command,
		Request:  req,
		Response: resp,
		PrevHash: w.prevHash,
	}
	h, err := r.Hash()
	if err != nil {
		return Record{}, fmt.Errorf("audit: hash: %w", err)
	}
	r.RecordHash = h
	line, err := r.MarshalWire()
	if err != nil {
		return Record{}, fmt.Errorf("audit: marshal: %w", err)
	}
	if _, err := w.w.Write(line); err != nil {
		return Record{}, fmt.Errorf("audit: write: %w", err)
	}
	if err := w.w.WriteByte('\n'); err != nil {
		return Record{}, fmt.Errorf("audit: write: %w", err)
	}
	if err := w.w.Flush(); err != nil {
		return Record{}, fmt.Errorf("audit: flush: %w", err)
	}
	w.prevHash = h
	w.seq++
	return r, nil
}

// Close flushes buffered data, fsyncs the file, and closes it.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.w.Flush(); err != nil {
		_ = w.f.Close()
		return fmt.Errorf("audit: flush: %w", err)
	}
	if err := w.f.Sync(); err != nil {
		_ = w.f.Close()
		return fmt.Errorf("audit: fsync: %w", err)
	}
	if err := w.f.Close(); err != nil {
		return fmt.Errorf("audit: close: %w", err)
	}
	return nil
}

// LastHash returns the most recent record hash written (or recovered
// from the file on open). Useful for tests and diagnostics.
func (w *Writer) LastHash() string { return w.prevHash }

// tailState reads the existing file line by line to find the last valid
// record. A missing file is not an error - it yields ("", 1, nil).
func tailState(path string) (prevHash string, nextSeq int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 1, nil
		}
		return "", 0, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var last Record
	var found bool
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			return "", 0, fmt.Errorf("parse line: %w", err)
		}
		last = r
		found = true
	}
	if err := sc.Err(); err != nil {
		return "", 0, err
	}
	if !found {
		return "", 1, nil
	}
	return last.RecordHash, last.Seq + 1, nil
}
