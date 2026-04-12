package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Verify(path)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.OK() {
		t.Fatalf("empty file should verify OK: %+v", res)
	}
	if res.Records != 0 {
		t.Fatalf("want 0 records, got %d", res.Records)
	}
}

func TestVerifyMissingFile(t *testing.T) {
	_, err := Verify("/nonexistent/path/file.jsonl")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestVerifySingleRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "single.jsonl")

	w, err := OpenWriter(path, Actor{Tool: "t", Version: "0", Host: "h"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := Request{Method: "GET", Path: "/test", Query: map[string]string{}}
	resp := Response{Status: 200, DurationMS: 1, BodySHA256: "x", Body: json.RawMessage(`{}`)}
	rec, err := w.Append("test", req, resp)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// First record should have empty prev_hash.
	if rec.PrevHash != "" {
		t.Fatalf("first record prev_hash should be empty, got %q", rec.PrevHash)
	}
	if rec.Seq != 1 {
		t.Fatalf("first record seq should be 1, got %d", rec.Seq)
	}

	res, err := Verify(path)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.OK() {
		t.Fatalf("single record should verify: %+v", res)
	}
	if res.Records != 1 {
		t.Fatalf("want 1 record, got %d", res.Records)
	}
}

func TestVerifyDetectsSequenceGap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gap.jsonl")

	w, err := OpenWriter(path, Actor{Tool: "t", Version: "0", Host: "h"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := Request{Method: "GET", Path: "/test", Query: map[string]string{}}
	resp := Response{Status: 200, DurationMS: 1, BodySHA256: "x", Body: json.RawMessage(`{}`)}
	for i := 0; i < 3; i++ {
		if _, err := w.Append("test", req, resp); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// Remove the second line to create a sequence gap.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := splitLines(string(raw))
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d", len(lines))
	}
	// Skip line[1] (seq=2), leaving seq 1 followed by seq 3.
	tampered := lines[0] + "\n" + lines[2] + "\n"
	if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Verify(path)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.OK() {
		t.Fatal("sequence gap should be detected")
	}
	if res.BrokenLine != 2 {
		t.Fatalf("want broken at line 2, got %d", res.BrokenLine)
	}
}

func TestVerifyDetectsPrevHashMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prevhash.jsonl")

	w, err := OpenWriter(path, Actor{Tool: "t", Version: "0", Host: "h"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := Request{Method: "GET", Path: "/test", Query: map[string]string{}}
	resp := Response{Status: 200, DurationMS: 1, BodySHA256: "x", Body: json.RawMessage(`{}`)}
	for i := 0; i < 2; i++ {
		if _, err := w.Append("test", req, resp); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	// Corrupt prev_hash of record 2 by replacing part of the hash.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := splitLines(string(raw))

	// Parse record 2, corrupt prev_hash, recompute record_hash so
	// only the chain link is broken, not the self-hash.
	var r Record
	if err := json.Unmarshal([]byte(lines[1]), &r); err != nil {
		t.Fatal(err)
	}
	r.PrevHash = "0000000000000000000000000000000000000000000000000000000000000000"
	h, err := r.Hash()
	if err != nil {
		t.Fatal(err)
	}
	r.RecordHash = h
	wire, err := r.MarshalWire()
	if err != nil {
		t.Fatal(err)
	}
	lines[1] = string(wire)
	if err := os.WriteFile(path, []byte(lines[0]+"\n"+lines[1]+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Verify(path)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.OK() {
		t.Fatal("prev_hash mismatch should be detected")
	}
	if res.BrokenAt != 2 {
		t.Fatalf("want broken at seq 2, got %d", res.BrokenAt)
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
