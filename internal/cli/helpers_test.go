package cli

import (
	"testing"
	"time"
)

// ---------- parseRelativeTime ----------

func TestParseRelativeTimeNow(t *testing.T) {
	now := time.Date(2026, 4, 12, 14, 0, 0, 0, time.UTC)
	for _, input := range []string{"", "now"} {
		got, err := parseRelativeTime(input, now)
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if !got.Equal(now) {
			t.Fatalf("input %q: want %v got %v", input, now, got)
		}
	}
}

func TestParseRelativeTimeGoDuration(t *testing.T) {
	now := time.Date(2026, 4, 12, 14, 0, 0, 0, time.UTC)
	got, err := parseRelativeTime("24h", now)
	if err != nil {
		t.Fatal(err)
	}
	want := now.Add(-24 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("want %v got %v", want, got)
	}
}

func TestParseRelativeTimeExtendedDays(t *testing.T) {
	now := time.Date(2026, 4, 12, 14, 0, 0, 0, time.UTC)
	cases := []struct {
		input string
		want  time.Duration
	}{
		{"7d", 7 * 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"1d12h", 36 * time.Hour},
	}
	for _, tc := range cases {
		got, err := parseRelativeTime(tc.input, now)
		if err != nil {
			t.Fatalf("%s: %v", tc.input, err)
		}
		want := now.Add(-tc.want)
		if !got.Equal(want) {
			t.Fatalf("%s: want %v got %v", tc.input, want, got)
		}
	}
}

func TestParseRelativeTimeRFC3339(t *testing.T) {
	now := time.Date(2026, 4, 12, 14, 0, 0, 0, time.UTC)
	input := "2026-04-01T00:00:00Z"
	got, err := parseRelativeTime(input, now)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("want %v got %v", want, got)
	}
}

func TestParseRelativeTimeInvalid(t *testing.T) {
	now := time.Date(2026, 4, 12, 14, 0, 0, 0, time.UTC)
	for _, input := range []string{"yesterday", "abc", "7x"} {
		_, err := parseRelativeTime(input, now)
		if err == nil {
			t.Fatalf("expected error for %q", input)
		}
	}
}

func TestParseRelativeTimeWhitespace(t *testing.T) {
	now := time.Date(2026, 4, 12, 14, 0, 0, 0, time.UTC)
	got, err := parseRelativeTime("  24h  ", now)
	if err != nil {
		t.Fatal(err)
	}
	want := now.Add(-24 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("want %v got %v", want, got)
	}
}

// ---------- parseExtendedDuration ----------

func TestParseExtendedDurationPure(t *testing.T) {
	got, err := parseExtendedDuration("30m")
	if err != nil {
		t.Fatal(err)
	}
	if got != 30*time.Minute {
		t.Fatalf("want 30m got %v", got)
	}
}

func TestParseExtendedDurationDaysOnly(t *testing.T) {
	got, err := parseExtendedDuration("7d")
	if err != nil {
		t.Fatal(err)
	}
	if got != 7*24*time.Hour {
		t.Fatalf("want 168h got %v", got)
	}
}

func TestParseExtendedDurationDaysPlusHours(t *testing.T) {
	got, err := parseExtendedDuration("2d6h")
	if err != nil {
		t.Fatal(err)
	}
	if got != 54*time.Hour {
		t.Fatalf("want 54h got %v", got)
	}
}

func TestParseExtendedDurationInvalid(t *testing.T) {
	_, err := parseExtendedDuration("xd")
	if err == nil {
		t.Fatal("expected error for xd")
	}
}

// ---------- formatPulseticTime ----------

func TestFormatPulseticTime(t *testing.T) {
	ts := time.Date(2026, 4, 12, 14, 30, 5, 0, time.UTC)
	got := formatPulseticTime(ts)
	want := "2026-04-12 14:30:05"
	if got != want {
		t.Fatalf("want %q got %q", want, got)
	}
}

func TestFormatPulseticTimeConvertsToUTC(t *testing.T) {
	loc := time.FixedZone("EST", -5*3600)
	ts := time.Date(2026, 4, 12, 10, 0, 0, 0, loc) // 10am EST = 15:00 UTC
	got := formatPulseticTime(ts)
	want := "2026-04-12 15:00:00"
	if got != want {
		t.Fatalf("want %q got %q", want, got)
	}
}

// ---------- readJSONInput ----------

func TestReadJSONInputLiteral(t *testing.T) {
	got, err := readJSONInput(`{"key":"val"}`, true)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"key":"val"}` {
		t.Fatalf("got %q", string(got))
	}
}

func TestReadJSONInputEmptyRequired(t *testing.T) {
	_, err := readJSONInput("", true)
	if err == nil {
		t.Fatal("expected error when empty and required")
	}
}

func TestReadJSONInputEmptyOptional(t *testing.T) {
	got, err := readJSONInput("", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %q", string(got))
	}
}

// ---------- parseID ----------

func TestParseIDValid(t *testing.T) {
	got, err := parseID("42")
	if err != nil {
		t.Fatal(err)
	}
	if got != 42 {
		t.Fatalf("want 42 got %d", got)
	}
}

func TestParseIDZero(t *testing.T) {
	_, err := parseID("0")
	if err == nil {
		t.Fatal("expected error for id 0")
	}
}

func TestParseIDNegative(t *testing.T) {
	_, err := parseID("-1")
	if err == nil {
		t.Fatal("expected error for negative id")
	}
}

func TestParseIDNonNumeric(t *testing.T) {
	_, err := parseID("abc")
	if err == nil {
		t.Fatal("expected error for non-numeric id")
	}
}
