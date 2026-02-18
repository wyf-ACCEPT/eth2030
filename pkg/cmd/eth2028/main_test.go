package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eth2028/eth2028/node"
)

func TestParseFlags_Defaults(t *testing.T) {
	cfg, exit, code := parseFlags([]string{})
	if exit {
		t.Fatalf("unexpected exit with code %d", code)
	}

	defaults := node.DefaultConfig()
	if cfg.DataDir != defaults.DataDir {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, defaults.DataDir)
	}
	if cfg.P2PPort != 30303 {
		t.Errorf("P2PPort = %d, want 30303", cfg.P2PPort)
	}
	if cfg.RPCPort != 8545 {
		t.Errorf("RPCPort = %d, want 8545", cfg.RPCPort)
	}
	if cfg.EnginePort != 8551 {
		t.Errorf("EnginePort = %d, want 8551", cfg.EnginePort)
	}
	if cfg.SyncMode != "snap" {
		t.Errorf("SyncMode = %q, want %q", cfg.SyncMode, "snap")
	}
	if cfg.NetworkID != 1 {
		t.Errorf("NetworkID = %d, want 1", cfg.NetworkID)
	}
	if cfg.MaxPeers != 50 {
		t.Errorf("MaxPeers = %d, want 50", cfg.MaxPeers)
	}
	if cfg.Verbosity != 3 {
		t.Errorf("Verbosity = %d, want 3", cfg.Verbosity)
	}
	if cfg.Metrics {
		t.Error("Metrics should be false by default")
	}
}

func TestParseFlags_AllFlags(t *testing.T) {
	args := []string{
		"-datadir", "/tmp/testdata",
		"-port", "30304",
		"-http.port", "9545",
		"-engine.port", "9551",
		"-syncmode", "full",
		"-networkid", "11155111",
		"-maxpeers", "25",
		"-verbosity", "4",
		"-metrics",
	}

	cfg, exit, _ := parseFlags(args)
	if exit {
		t.Fatal("unexpected exit")
	}

	if cfg.DataDir != "/tmp/testdata" {
		t.Errorf("DataDir = %q, want /tmp/testdata", cfg.DataDir)
	}
	if cfg.P2PPort != 30304 {
		t.Errorf("P2PPort = %d, want 30304", cfg.P2PPort)
	}
	if cfg.RPCPort != 9545 {
		t.Errorf("RPCPort = %d, want 9545", cfg.RPCPort)
	}
	if cfg.EnginePort != 9551 {
		t.Errorf("EnginePort = %d, want 9551", cfg.EnginePort)
	}
	if cfg.SyncMode != "full" {
		t.Errorf("SyncMode = %q, want full", cfg.SyncMode)
	}
	if cfg.NetworkID != 11155111 {
		t.Errorf("NetworkID = %d, want 11155111", cfg.NetworkID)
	}
	if cfg.MaxPeers != 25 {
		t.Errorf("MaxPeers = %d, want 25", cfg.MaxPeers)
	}
	if cfg.Verbosity != 4 {
		t.Errorf("Verbosity = %d, want 4", cfg.Verbosity)
	}
	if !cfg.Metrics {
		t.Error("Metrics should be true")
	}
}

func TestParseFlags_DoubleDash(t *testing.T) {
	// The flag package accepts both -flag and --flag.
	args := []string{
		"--port", "30305",
		"--syncmode", "full",
		"--metrics",
	}

	cfg, exit, _ := parseFlags(args)
	if exit {
		t.Fatal("unexpected exit")
	}
	if cfg.P2PPort != 30305 {
		t.Errorf("P2PPort = %d, want 30305", cfg.P2PPort)
	}
	if cfg.SyncMode != "full" {
		t.Errorf("SyncMode = %q, want full", cfg.SyncMode)
	}
	if !cfg.Metrics {
		t.Error("Metrics should be true with --metrics")
	}
}

func TestParseFlags_Version(t *testing.T) {
	_, exit, code := parseFlags([]string{"-version"})
	if !exit {
		t.Fatal("expected exit for -version")
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestParseFlags_InvalidFlag(t *testing.T) {
	_, exit, code := parseFlags([]string{"-unknown-flag"})
	if !exit {
		t.Fatal("expected exit for unknown flag")
	}
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestParseFlags_InvalidNetworkID(t *testing.T) {
	_, exit, code := parseFlags([]string{"-networkid", "notanumber"})
	if !exit {
		t.Fatal("expected exit for invalid networkid")
	}
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestParseFlags_PartialOverride(t *testing.T) {
	// Only override a single flag; everything else keeps defaults.
	cfg, exit, _ := parseFlags([]string{"-maxpeers", "100"})
	if exit {
		t.Fatal("unexpected exit")
	}
	if cfg.MaxPeers != 100 {
		t.Errorf("MaxPeers = %d, want 100", cfg.MaxPeers)
	}
	// Verify other defaults are untouched.
	if cfg.P2PPort != 30303 {
		t.Errorf("P2PPort = %d, want 30303", cfg.P2PPort)
	}
	if cfg.SyncMode != "snap" {
		t.Errorf("SyncMode = %q, want snap", cfg.SyncMode)
	}
}

func TestVerbosityMapping(t *testing.T) {
	tests := []struct {
		verbosity int
		wantLevel string
	}{
		{0, "error"},
		{1, "error"},
		{2, "warn"},
		{3, "info"},
		{4, "debug"},
		{5, "debug"},
	}
	for _, tt := range tests {
		got := node.VerbosityToLogLevel(tt.verbosity)
		if got != tt.wantLevel {
			t.Errorf("VerbosityToLogLevel(%d) = %q, want %q", tt.verbosity, got, tt.wantLevel)
		}
	}
}

func TestInitDataDir(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "eth2028-test")

	cfg := node.DefaultConfig()
	cfg.DataDir = dir

	if err := cfg.InitDataDir(); err != nil {
		t.Fatalf("InitDataDir() error: %v", err)
	}

	// Verify root directory exists.
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("datadir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("datadir is not a directory")
	}

	// Verify subdirectories.
	for _, sub := range []string{"chaindata", "keystore", "nodes"} {
		subpath := filepath.Join(dir, sub)
		info, err := os.Stat(subpath)
		if err != nil {
			t.Errorf("subdir %q not created: %v", sub, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("subdir %q is not a directory", sub)
		}
	}
}

func TestInitDataDir_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "eth2028-test")

	cfg := node.DefaultConfig()
	cfg.DataDir = dir

	// Initialize twice; second call should not fail.
	if err := cfg.InitDataDir(); err != nil {
		t.Fatalf("first InitDataDir() error: %v", err)
	}
	if err := cfg.InitDataDir(); err != nil {
		t.Fatalf("second InitDataDir() error: %v", err)
	}
}

func TestInitDataDir_EmptyPath(t *testing.T) {
	cfg := node.DefaultConfig()
	cfg.DataDir = ""
	if err := cfg.InitDataDir(); err == nil {
		t.Fatal("expected error for empty datadir")
	}
}
