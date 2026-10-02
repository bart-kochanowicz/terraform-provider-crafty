package client

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"syscall"
	"testing"
	"time"
)

func TestRetryableReadFailures(t *testing.T) {
	for _, code := range []int{408, 429, 500, 502, 503, 504, 400, 401, 403, 404, 409, 501, 302} {
		want := code == 408 || code == 429 || code == 500 || code == 502 || code == 503 || code == 504
		if got := IsRetryableRead(&APIError{StatusCode: code}); got != want {
			t.Errorf("HTTP %d retry=%v, want %v", code, got, want)
		}
	}
	for _, tc := range []struct {
		err   error
		retry bool
	}{
		{io.ErrUnexpectedEOF, true}, {io.EOF, true}, {syscall.ECONNRESET, true},
		{syscall.ECONNREFUSED, true}, {&net.DNSError{IsTemporary: true}, true},
		{context.DeadlineExceeded, true}, {context.Canceled, false},
		{errors.New("decode API response"), false}, {&net.DNSError{IsNotFound: true}, false}, {nil, false},
	} {
		if got := IsRetryableRead(tc.err); got != tc.retry {
			t.Errorf("%v retry=%v, want %v", tc.err, got, tc.retry)
		}
	}
}
func TestRetryAfter(t *testing.T) {
	for _, value := range []string{"", "invalid", "-1", "99999999999999999"} {
		if got := retryAfter(value); got != 0 {
			t.Errorf("invalid header %q: %v", value, got)
		}
	}
	if retryAfter("5") != 5*time.Second {
		t.Fatal("seconds ignored")
	}
	if delay := retryAfter(time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)); delay < 59*time.Second || delay > time.Minute {
		t.Fatalf("HTTP date ignored: %v", delay)
	}
}
