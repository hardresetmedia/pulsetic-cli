package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// VerifyResult describes the outcome of replaying a JSONL audit file's
// hash chain. If BrokenAt is non-zero, the file was tampered with - the
// first broken record is reported.
type VerifyResult struct {
	Records    int
	LastHash   string
	BrokenAt   int64  // seq of the first broken record; 0 = chain intact
	BrokenLine int    // 1-indexed line number of the break
	Reason     string // human-readable description of the break
}

// OK reports whether the chain is intact.
func (r VerifyResult) OK() bool { return r.BrokenAt == 0 }

// Verify walks every record in the audit file at path, recomputing hashes
// and checking sequence/prev_hash links. The first detected break halts
// the walk and is reported in the result - remaining records are not
// inspected.
func Verify(path string) (VerifyResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("verify: open: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)

	res := VerifyResult{}
	expectedPrev := ""
	expectedSeq := int64(1)
	lineNum := 0

	for sc.Scan() {
		lineNum++
		raw := sc.Bytes()
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}

		var r Record
		if err := json.Unmarshal(raw, &r); err != nil {
			return res, fmt.Errorf("verify: line %d: parse: %w", lineNum, err)
		}

		if r.Seq != expectedSeq {
			res.BrokenAt = r.Seq
			res.BrokenLine = lineNum
			res.Reason = fmt.Sprintf("seq mismatch: expected %d, got %d", expectedSeq, r.Seq)
			return res, nil
		}
		if r.PrevHash != expectedPrev {
			res.BrokenAt = r.Seq
			res.BrokenLine = lineNum
			res.Reason = fmt.Sprintf("prev_hash mismatch: expected %q, got %q", expectedPrev, r.PrevHash)
			return res, nil
		}

		recomputed, err := r.Hash()
		if err != nil {
			return res, fmt.Errorf("verify: line %d: hash: %w", lineNum, err)
		}
		if r.RecordHash != recomputed {
			res.BrokenAt = r.Seq
			res.BrokenLine = lineNum
			res.Reason = fmt.Sprintf("record_hash mismatch: expected %q, got %q", recomputed, r.RecordHash)
			return res, nil
		}

		expectedPrev = r.RecordHash
		expectedSeq = r.Seq + 1
		res.Records++
		res.LastHash = r.RecordHash
	}
	if err := sc.Err(); err != nil {
		return res, fmt.Errorf("verify: scan: %w", err)
	}
	return res, nil
}
