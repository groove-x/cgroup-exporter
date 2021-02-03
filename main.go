package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/containerd/cgroups"
	"github.com/docker/docker/api/types"
	"github.com/moby/moby/client"
)

var (
	versionFlag = flag.Bool("version", false, "version")
	version     string
	git         string

	address      = flag.String("address", ":48900", "address")
	cgroupPath   = flag.String("cgroup-path", "/system.slice", "path to cgroup")
	enableDocker = flag.Bool("metrics.docker", false, "docker container metrics")
)

func main() {
	flag.Parse()
	if *versionFlag {
		fmt.Printf("version %s, git %s\n", version, git)
		return
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

	system, err := cgroups.Load(subsystem, cgroups.StaticPath(*cgroupPath))
	if err != nil {
		log.Fatalf("cgroups load: %s", err)
	}

	var dockerClient *client.Client
	if *enableDocker {
		dockerClient, err = client.NewEnvClient()
		if err != nil {
			log.Fatalf("%v", err)
		}
		defer dockerClient.Close()
	}

	http.HandleFunc("/metrics", exportMetrics(system, dockerClient))

	server := &http.Server{
		Addr: *address,
	}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http server ListenAndServe: %v", err)
		}
	}()

	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("http server shutdown: %s", err)
	}
}

type ProcessStats struct {
	FdCount     int `json:"fd_count"`
	SocketCount int `json:"socket_count"`
}

func processStats(pid int) *ProcessStats {
	dir := fmt.Sprintf("/proc/%d/fd", pid)
	fds, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil
	}
	var socketCount int
	for _, fd := range fds {
		fdPath := path.Join(dir, fd.Name())
		linkName, err := os.Readlink(fdPath)
		if err != nil {
			continue
		}
		if strings.HasPrefix(linkName, "socket") {
			socketCount++
		}
	}
	return &ProcessStats{
		FdCount:     len(fds),
		SocketCount: socketCount,
	}
}

func subsystem() ([]cgroups.Subsystem, error) {
	root := "/sys/fs/cgroup"
	s := []cgroups.Subsystem{
		cgroups.NewDevices(root),
		cgroups.NewCpuacct(root),
		cgroups.NewMemory(root),
	}
	return s, nil
}

type cgroupMetrics struct {
	*cgroups.Metrics
	Process *ProcessStats `json:"process_stats"`
}

func statsCgroups(ctx context.Context, system cgroups.Cgroup) (map[string]*cgroupMetrics, error) {
	processes, err := system.Processes(cgroups.Devices, true)
	if err != nil {
		return nil, fmt.Errorf("cgroups load: %s", err)
	}

	groups := make(map[string]*cgroupMetrics, len(processes))
	for _, p := range processes {
		name := strings.TrimPrefix(p.Path, "/sys/fs/cgroup/devices")
		name = strings.TrimSuffix(name, "/")
		if _, ok := groups[name]; ok {
			continue
		}

		control, err := cgroups.Load(subsystem, func(subsystem cgroups.Name) (string, error) {
			return name, nil
		})
		if err != nil {
			log.Printf("cgroups load: %s", err)
			continue
		}
		stats, err := control.Stat(cgroups.IgnoreNotExist)
		if err != nil {
			log.Printf("control stat: %s", err)
			continue
		}
		ps := processStats(p.Pid)
		groups[name] = &cgroupMetrics{
			Metrics: stats,
			Process: ps,
		}
	}
	return groups, nil
}

type dockerStats struct {
	CPU     types.CPUStats    `json:"cpu_stats,omitempty"`
	PreCPU  types.CPUStats    `json:"precpu_stats,omitempty"` // "Pre"="Previous"
	Memory  types.MemoryStats `json:"memory_stats,omitempty"`
	Process *ProcessStats     `json:"process_stats"`
}

func statsDockerContainers(ctx context.Context, dockerClient *client.Client) (map[string]dockerStats, error) {
	if dockerClient == nil {
		return nil, nil
	}
	containers, err := dockerClient.ContainerList(ctx, types.ContainerListOptions{
		All:   true,
		Limit: 0,
	})
	if err != nil {
		return nil, fmt.Errorf("list docker containers: %s", err)
	}

	dockerContainers := make(map[string]dockerStats, len(containers))
	for _, container := range containers {
		res, err := dockerClient.ContainerStats(ctx, container.ID, false)
		if err != nil {
			log.Printf("failed to stats docker container %s: %s", container.ID, err)
			continue
		}
		var stats dockerStats
		if err := json.NewDecoder(res.Body).Decode(&stats); err != nil {
			res.Body.Close()
			return nil, fmt.Errorf("failed to decode stats json: %s", err)
		}
		res.Body.Close()
		name := fmt.Sprintf("/docker%s", strings.Join(container.Names, "/"))
		inspect, err := dockerClient.ContainerInspect(ctx, container.ID)
		if err == nil {
			if inspect.State != nil {
				stats.Process = processStats(inspect.State.Pid)
			}
		}
		dockerContainers[name] = stats
	}

	return dockerContainers, nil
}

func exportMetrics(system cgroups.Cgroup, dockerClient *client.Client) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		groups, err := statsCgroups(ctx, system)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		dockerContainers, err := statsDockerContainers(ctx, dockerClient)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		fmt.Fprintln(w, `# HELP container_cpu_user_seconds_total Cumulative user cpu time consumed in seconds.
# TYPE container_cpu_user_seconds_total counter`)
		for name, stats := range groups {
			fmt.Fprintf(w, `container_cpu_user_seconds_total{id=%s} %.2f`, strconv.Quote(name), float64(stats.CPU.Usage.User)/1000000000.0)
			fmt.Fprintln(w)
		}
		for name, stats := range dockerContainers {
			fmt.Fprintf(w, `container_cpu_user_seconds_total{id=%s} %.2f`, strconv.Quote(name), float64(stats.CPU.CPUUsage.UsageInUsermode)/1000000000.0)
			fmt.Fprintln(w)
		}

		fmt.Fprintln(w, `# HELP container_memory_usage_bytes Current memory usage in bytes, including all memory regardless of when it was accessed
# TYPE container_memory_usage_bytes gauge`)
		for name, stats := range groups {
			fmt.Fprintf(w, `container_memory_usage_bytes{id=%s} %d`, strconv.Quote(name), stats.Memory.Usage.Usage)
			fmt.Fprintln(w)
		}
		for name, stats := range dockerContainers {
			fmt.Fprintf(w, `container_memory_usage_bytes{id=%s} %d`, strconv.Quote(name), stats.Memory.Usage)
			fmt.Fprintln(w)
		}

		fmt.Fprintln(w, `# HELP container_memory_rss Size of RSS in bytes.
# TYPE container_memory_rss gauge`)
		for name, stats := range groups {
			fmt.Fprintf(w, `container_memory_rss{id=%s} %d`, strconv.Quote(name), stats.Memory.RSS)
			fmt.Fprintln(w)
		}
		for name, stats := range dockerContainers {
			fmt.Fprintf(w, `container_memory_rss{id=%s} %d`, strconv.Quote(name), stats.Memory.Stats["rss"])
			fmt.Fprintln(w)
		}

		fmt.Fprintln(w, `# HELP container_open_fds Number of open file descriptors
# TYPE container_open_fds gauge`)
		for name, stats := range groups {
			if stats.Process == nil {
				continue
			}
			fmt.Fprintf(w, `container_open_fds{id=%s} %d`, strconv.Quote(name), stats.Process.FdCount)
			fmt.Fprintln(w)
		}
		for name, stats := range dockerContainers {
			if stats.Process == nil {
				continue
			}
			fmt.Fprintf(w, `container_open_fds{id=%s} %d`, strconv.Quote(name), stats.Process.FdCount)
			fmt.Fprintln(w)
		}

		fmt.Fprintln(w, `# HELP container_open_sockets Number of open sockets
# TYPE container_open_sockets gauge`)
		for name, stats := range groups {
			if stats.Process == nil {
				continue
			}
			fmt.Fprintf(w, `container_open_sockets{id=%s} %d`, strconv.Quote(name), stats.Process.SocketCount)
			fmt.Fprintln(w)
		}
		for name, stats := range dockerContainers {
			if stats.Process == nil {
				continue
			}
			fmt.Fprintf(w, `container_open_sockets{id=%s} %d`, strconv.Quote(name), stats.Process.SocketCount)
			fmt.Fprintln(w)
		}

		processStats := processStats(os.Getpid())
		if processStats != nil {
			fmt.Fprintln(w, `# HELP process_open_fds Number of open file descriptors
# TYPE process_open_fds gauge`)
			fmt.Fprintf(w, `process_open_fds %d`, processStats.FdCount)
			fmt.Fprintln(w)
		}

		return
	}

}
