package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/cgroups/v3/cgroup2"
)

func TestCgroup2StatReadsCPUWithoutCPUController(t *testing.T) {
	mountpoint := t.TempDir()
	dir := filepath.Join(mountpoint, "system.slice", "foo.service")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// A service cgroup where systemd has not delegated the cpu controller.
	files := map[string]string{
		"cgroup.controllers": "memory pids\n",
		"cpu.stat":           "usage_usec 5000000\nuser_usec 3000000\nsystem_usec 2000000\nnr_periods 0\nnr_throttled 0\nthrottled_usec 0\n",
		"memory.stat":        "anon 1024\nfile 2048\n",
		"memory.current":     "4096\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	manager, err := cgroup2.Load("/system.slice/foo.service", cgroup2.WithMountpoint(mountpoint))
	if err != nil {
		t.Fatal(err)
	}
	got, err := manager.StatFiltered(cgroup2.StatCPU | cgroup2.StatMemory)
	if err != nil {
		t.Fatal(err)
	}
	if got.CPU.UsageUsec != 5000000 || got.CPU.UserUsec != 3000000 || got.CPU.SystemUsec != 2000000 {
		t.Errorf("unexpected cpu stat: %+v", got.CPU)
	}
	if got.Memory.Anon != 1024 || got.Memory.Usage != 4096 {
		t.Errorf("unexpected memory stat: %+v", got.Memory)
	}
}
