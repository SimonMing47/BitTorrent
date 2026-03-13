package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunRejectsMissingRequiredFlags(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := run([]string{"-i", "sample.torrent"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "用法: btclient -i input.torrent -o output.file [-tls-path cert.pem]") {
		t.Fatalf("unexpected usage output: %q", stderr.String())
	}
}
