package client

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
)

// IsRetryableRead classifies transient failures. Callers must only retry reads;
// replaying a mutation after a lost response can create duplicate resources.
func IsRetryableRead(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var api *APIError
	if errors.As(err, &api) {
		switch api.StatusCode {
		case 408, 429, 500, 502, 503, 504:
			return true
		default:
			return false
		}
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return true
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return dns.IsTemporary
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE)
}
