// Package audit implements the append-only, hash-chained JSONL audit log
// used by pulsetic-cli to record every API call made against Pulsetic.
//
// Each line in the log is one Record. Records reference the SHA-256 of the
// prior record via PrevHash, forming a chain that can be replayed to detect
// tampering. See Verify for the replay logic.
package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Record is a single audit entry. The Response.Body is kept as json.RawMessage
// so the exact bytes returned by Pulsetic are preserved - they are the evidence.
// The record itself is canonicalised (sorted keys) at write time so the hash
// chain is reproducible.
type Record struct {
	Seq        int64    `json:"seq"`
	TS         string   `json:"ts"`
	Actor      Actor    `json:"actor"`
	Command    string   `json:"command"`
	Request    Request  `json:"request"`
	Response   Response `json:"response"`
	PrevHash   string   `json:"prev_hash"`
	RecordHash string   `json:"record_hash"`
}

// Actor identifies the process that produced a record.
type Actor struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	Host    string `json:"host"`
}

// Request captures the outbound HTTP call. Headers are intentionally omitted
// so the Authorization token can never leak into the log.
type Request struct {
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Query  map[string]string `json:"query"`
}

// Response captures the inbound HTTP response. BodySHA256 is the hash of the
// raw bytes received, independent of the canonicalised Body representation.
type Response struct {
	Status     int             `json:"status"`
	DurationMS int64           `json:"duration_ms"`
	BodySHA256 string          `json:"body_sha256"`
	Body       json.RawMessage `json:"body"`
}

// Hash returns the hex-encoded SHA-256 of the record's canonical form,
// excluding the record_hash field itself. This is what gets stored in
// RecordHash (for this record) and PrevHash (for the next record).
func (r *Record) Hash() (string, error) {
	b, err := r.canonicalBytesWithoutHash()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// MarshalWire produces the canonical JSON line that is appended to the
// audit log file. RecordHash must already be set to the value returned
// by Hash().
func (r *Record) MarshalWire() ([]byte, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return canonicalize(raw)
}

func (r *Record) canonicalBytesWithoutHash() ([]byte, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("canonicalize: decode: %w", err)
	}
	delete(m, "record_hash")
	var buf bytes.Buffer
	if err := writeCanonical(&buf, m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// canonicalize re-serialises a JSON byte slice with object keys sorted
// lexicographically and no insignificant whitespace. This is the
// minimum-viable "JCS-lite" needed for deterministic hashing of our
// record shape.
func canonicalize(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("canonicalize: decode: %w", err)
	}
	var buf bytes.Buffer
	if err := writeCanonical(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonical(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case string:
		b, err := json.Marshal(x)
		if err != nil {
			return err
		}
		buf.Write(b)
	case json.Number:
		buf.WriteString(x.String())
	case float64:
		// Numbers only reach here if the caller did not use UseNumber.
		// Round-trip via json.Marshal for JSON-compliant formatting.
		b, err := json.Marshal(x)
		if err != nil {
			return err
		}
		buf.Write(b)
	case []any:
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return err
			}
			buf.Write(kb)
			buf.WriteByte(':')
			if err := writeCanonical(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("canonicalize: unsupported type %T", v)
	}
	return nil
}
