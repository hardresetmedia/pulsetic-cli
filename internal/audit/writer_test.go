package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func fixedActor() Actor {
	return Actor{Tool: "pulsetic-cli", Version: "0.1.0", Host: "test-host"}
}

func fixedRequest() Request {
	return Request{
		Method: "GET",
		Path:   "/monitors",
		Query:  map[string]string{"page": "1", "per_page": "20"},
	}
}

func fixedResponse() Response {
	return Response{
		Status:     200,
		DurationMS: 42,
		BodySHA256: "deadbeef",
		Body:       json.RawMessage(`{"monitors":[{"id":1,"url":"https://w5.com"}]}`),
	}
}

func TestAppendAndVerify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pulsetic-2026-04.jsonl")

	clock := fixedClock(time.Date(2026, 4, 11, 14, 30, 0, 0, time.UTC))
	w, err := OpenWriter(path, fixedActor(), clock)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}

	for i := 0; i < 3; i++ {
		if _, err := w.Append("monitors.list", fixedRequest(), fixedResponse()); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	res, err := Verify(path)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.OK() {
		t.Fatalf("chain broken: at seq=%d line=%d: %s", res.BrokenAt, res.BrokenLine, res.Reason)
	}
	if res.Records != 3 {
		t.Fatalf("want 3 records, got %d", res.Records)
	}
	if res.LastHash == "" {
		t.Fatal("last hash empty")
	}
}

func TestResumeContinuesChain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	clock := fixedClock(time.Date(2026, 4, 11, 14, 30, 0, 0, time.UTC))

	// First session: write 2 records.
	w1, err := OpenWriter(path, fixedActor(), clock)
	if err != nil {
		t.Fatalf("OpenWriter 1: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := w1.Append("c", fixedRequest(), fixedResponse()); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	lastHash1 := w1.LastHash()
	if err := w1.Close(); err != nil {
		t.Fatalf("close 1: %v", err)
	}

	// Second session: reopen and confirm prev_hash + seq resume.
	w2, err := OpenWriter(path, fixedActor(), clock)
	if err != nil {
		t.Fatalf("OpenWriter 2: %v", err)
	}
	if w2.LastHash() != lastHash1 {
		t.Fatalf("reopen lost chain: want %q got %q", lastHash1, w2.LastHash())
	}
	r, err := w2.Append("c", fixedRequest(), fixedResponse())
	if err != nil {
		t.Fatalf("append after reopen: %v", err)
	}
	if r.Seq != 3 {
		t.Fatalf("want seq 3, got %d", r.Seq)
	}
	if r.PrevHash != lastHash1 {
		t.Fatalf("prev_hash not linked: want %q got %q", lastHash1, r.PrevHash)
	}
	if err := w2.Close(); err != nil {
		t.Fatalf("close 2: %v", err)
	}

	res, err := Verify(path)
	if err != nil || !res.OK() {
		t.Fatalf("verify after resume: res=%+v err=%v", res, err)
	}
	if res.Records != 3 {
		t.Fatalf("want 3 records, got %d", res.Records)
	}
}

func TestTamperDetected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	clock := fixedClock(time.Date(2026, 4, 11, 14, 30, 0, 0, time.UTC))

	w, err := OpenWriter(path, fixedActor(), clock)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := w.Append("c", fixedRequest(), fixedResponse()); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Tamper: change the body of record 2 (line 2).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d", len(lines))
	}
	lines[1] = strings.Replace(lines[1], "w5.com", "w6.com", 1)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	res, err := Verify(path)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.OK() {
		t.Fatal("tampering not detected")
	}
	if res.BrokenAt != 2 {
		t.Fatalf("want break at seq 2, got %d (reason: %s)", res.BrokenAt, res.Reason)
	}
	if res.BrokenLine != 2 {
		t.Fatalf("want broken line 2, got %d", res.BrokenLine)
	}
}

func TestHashIsStableAcrossMapOrder(t *testing.T) {
	// Same logical record, map populated in two different orders.
	// The canonicaliser must produce identical hashes.
	mkRecord := func(q map[string]string) Record {
		return Record{
			Seq:      1,
			TS:       "2026-04-11T14:30:00Z",
			Actor:    fixedActor(),
			Command:  "c",
			Request:  Request{Method: "GET", Path: "/monitors", Query: q},
			Response: fixedResponse(),
			PrevHash: "",
		}
	}

	a := mkRecord(map[string]string{"a": "1", "b": "2", "c": "3"})
	b := mkRecord(map[string]string{"c": "3", "a": "1", "b": "2"})

	ha, err := a.Hash()
	if err != nil {
		t.Fatalf("hash a: %v", err)
	}
	hb, err := b.Hash()
	if err != nil {
		t.Fatalf("hash b: %v", err)
	}
	if ha != hb {
		t.Fatalf("map order affected hash: %q vs %q", ha, hb)
	}
}

func TestHashExcludesRecordHashField(t *testing.T) {
	r := Record{
		Seq: 1, TS: "2026-04-11T14:30:00Z", Actor: fixedActor(),
		Command: "c", Request: fixedRequest(), Response: fixedResponse(),
	}
	h1, err := r.Hash()
	if err != nil {
		t.Fatal(err)
	}
	// Setting record_hash to something must not change the computed hash.
	r.RecordHash = "not-a-real-hash"
	h2, err := r.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("Hash() must exclude record_hash field: %q vs %q", h1, h2)
	}
}
