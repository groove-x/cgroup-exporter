package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadCPUStatV2WithoutCPUController(t *testing.T) {
	dir := t.TempDir()
	// A service cgroup where systemd has not delegated the cpu controller.
	if err := os.WriteFile(filepath.Join(dir, "cgroup.controllers"), []byte("memory pids\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cpuStat := "usage_usec 5000000\nuser_usec 3000000\nsystem_usec 2000000\nnr_periods 0\nnr_throttled 0\nthrottled_usec 0\n"
	if err := os.WriteFile(filepath.Join(dir, "cpu.stat"), []byte(cpuStat), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := readCPUStatV2(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsageUsec != 5000000 || got.UserUsec != 3000000 || got.SystemUsec != 2000000 {
		t.Errorf("unexpected cpu stat: %+v", got)
	}
}

func TestReadCPUStatV2MissingFile(t *testing.T) {
	if _, err := readCPUStatV2(t.TempDir()); !os.IsNotExist(err) {
		t.Errorf("expected not-exist error, got %v", err)
	}
}
