package pulsetic

import (
	"testing"
)

func TestExtractArrayEmpty(t *testing.T) {
	items, err := ExtractArray([]byte(`{"data":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("want 0 items, got %d", len(items))
	}
}

func TestExtractArrayNilBody(t *testing.T) {
	items, err := ExtractArray(nil)
	if err != nil {
		t.Fatal(err)
	}
	if items != nil {
		t.Fatalf("want nil, got %v", items)
	}
}

func TestExtractArrayEmptyBytes(t *testing.T) {
	items, err := ExtractArray([]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if items != nil {
		t.Fatalf("want nil, got %v", items)
	}
}

func TestExtractArrayInvalidJSON(t *testing.T) {
	_, err := ExtractArray([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestExtractArrayScalar(t *testing.T) {
	// A response that's just a number or string, not an array or object.
	_, err := ExtractArray([]byte(`42`))
	if err == nil {
		t.Fatal("expected error for scalar JSON")
	}
}

func TestExtractMonitorIDsSkipsZeroID(t *testing.T) {
	// An item with id:0 should be skipped (e.g. partially constructed object).
	ids, err := ExtractMonitorIDs([]byte(`[{"id":1},{"id":0},{"id":3}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != 1 || ids[1] != 3 {
		t.Fatalf("want [1 3], got %v", ids)
	}
}

func TestExtractStatusPageIDsDataEnvelope(t *testing.T) {
	ids, err := ExtractStatusPageIDs([]byte(`{"data":[{"id":10},{"id":20}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != 10 || ids[1] != 20 {
		t.Fatalf("want [10 20], got %v", ids)
	}
}

func TestExtractStatusPageIDsBareArray(t *testing.T) {
	ids, err := ExtractStatusPageIDs([]byte(`[{"id":5}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != 5 {
		t.Fatalf("want [5], got %v", ids)
	}
}
