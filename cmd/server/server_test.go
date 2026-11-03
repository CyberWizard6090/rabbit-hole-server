package main

import (
	"net/http"
	"testing"
)

func TestNewHTTPServerSetsEveryTimeout(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", http.NewServeMux())

	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 || srv.IdleTimeout <= 0 {
		t.Fatalf("a timeout is unset: %+v", srv)
	}
	if srv.ReadHeaderTimeout >= srv.ReadTimeout {
		t.Fatalf("ReadHeaderTimeout (%v) must be shorter than ReadTimeout (%v)", srv.ReadHeaderTimeout, srv.ReadTimeout)
	}
	if srv.MaxHeaderBytes <= 0 || srv.MaxHeaderBytes >= http.DefaultMaxHeaderBytes {
		t.Fatalf("MaxHeaderBytes = %d, want a limit below the %d default", srv.MaxHeaderBytes, http.DefaultMaxHeaderBytes)
	}
}
