package pulsetic

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := New(Options{
		BaseURL:   srv.URL + "/api/public",
		Token:     "test-token",
		Timeout:   5 * time.Second,
		RetryMax:  3,
		RetryBase: 1 * time.Millisecond, // keep tests fast
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, srv
}

func TestDoSendsRawTokenInAuthHeader(t *testing.T) {
	// Pulsetic's auth format is unusual: the raw token goes in the
	// Authorization header with no "Bearer " prefix. This test pins that
	// behaviour against regressions.
	var seen string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"data":[]}`))
	})

	call, err := c.Do(context.Background(), "GET", "/monitors", nil, nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if seen != "test-token" {
		t.Fatalf("auth header: want %q got %q", "test-token", seen)
	}
	if call.Status != 200 {
		t.Fatalf("status: want 200 got %d", call.Status)
	}
	if call.BodySHA256 == "" {
		t.Fatal("body sha256 not set")
	}
}

func TestDoRetriesOn429ThenSucceeds(t *testing.T) {
	var calls int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"data":[{"id":1}]}`))
	})

	call, err := c.Do(context.Background(), "GET", "/monitors", nil, nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if call.Status != 200 {
		t.Fatalf("status: want 200 got %d", call.Status)
	}
	if call.Attempts != 3 {
		t.Fatalf("attempts: want 3 got %d", call.Attempts)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("server hits: want 3 got %d", got)
	}
}

func TestDoReturnsLastCallWhenRetriesExhausted(t *testing.T) {
	var calls int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(500)
	})

	call, err := c.Do(context.Background(), "GET", "/monitors", nil, nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if call.Status != 500 {
		t.Fatalf("want final status 500, got %d", call.Status)
	}
	// 1 initial + RetryMax=3 retries = 4 attempts
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Fatalf("server hits: want 4 got %d", got)
	}
}

func TestDoIncludesSortedQueryString(t *testing.T) {
	var seenRawQuery string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		seenRawQuery = r.URL.RawQuery
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"data":[]}`))
	})

	_, err := c.Do(context.Background(), "GET", "/monitors", map[string]string{
		"per_page": "50",
		"page":     "1",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "page=1&per_page=50"
	if seenRawQuery != want {
		t.Fatalf("query: want %q got %q", want, seenRawQuery)
	}
}

func TestDoRetriesPostWithBody(t *testing.T) {
	// Verifies that POST retries re-send the request body correctly.
	// bytes.NewReader is re-created each attempt (client.go:118-119),
	// so the body must arrive intact on the successful attempt.
	var calls int32
	var lastBody string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		b, _ := io.ReadAll(r.Body)
		lastBody = string(b)
		if n < 3 {
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"success":true}`))
	})

	body := []byte(`{"urls":["https://example.com"],"add_account_email":true}`)
	call, err := c.Do(context.Background(), "POST", "/monitors", nil, body)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if call.Status != 200 {
		t.Fatalf("status: want 200 got %d", call.Status)
	}
	if call.Attempts != 3 {
		t.Fatalf("attempts: want 3 got %d", call.Attempts)
	}
	// The body must have arrived intact on the final (successful) attempt.
	if lastBody != string(body) {
		t.Fatalf("body on final attempt: want %q got %q", string(body), lastBody)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("server hits: want 3 got %d", got)
	}
}

func TestExtractMonitorIDsHandlesBothEnvelopes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []int64
	}{
		{"data envelope", `{"data":[{"id":1},{"id":2}]}`, []int64{1, 2}},
		{"bare array", `[{"id":10},{"id":20}]`, []int64{10, 20}},
		{"empty envelope", `{"data":[]}`, []int64{}},
		{"empty array", `[]`, []int64{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractMonitorIDs([]byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("want %v got %v", tc.want, got)
			}
		})
	}
}
