package main

import (
	"bytes"
	"os"
	"path/filepath"
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
	if !strings.Contains(stderr.String(), "用法: btclient -i input.torrent -o output.dir [-tls-path cert.pem]") {
		t.Fatalf("unexpected usage output: %q", stderr.String())
	}
}

func TestBuildOutputPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "downloads")
	target, err := buildOutputPath(dir, "ubuntu.iso")
	if err != nil {
		t.Fatalf("buildOutputPath() error = %v", err)
	}

	if target != filepath.Join(dir, "ubuntu.iso") {
		t.Fatalf("unexpected target path: %q", target)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("os.Stat() error = %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %q to be a directory", dir)
	}
}

func TestBuildOutputPathUsesBaseName(t *testing.T) {
	dir := t.TempDir()
	target, err := buildOutputPath(dir, "nested/path/debian.iso")
	if err != nil {
		t.Fatalf("buildOutputPath() error = %v", err)
	}

	if target != filepath.Join(dir, "debian.iso") {
		t.Fatalf("unexpected sanitized target path: %q", target)
	}
}
