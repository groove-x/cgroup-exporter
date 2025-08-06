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
	v1 "github.com/containerd/cgroups/stats/v1"
	v2 "github.com/containerd/cgroups/v2"
	"github.com/containerd/cgroups/v2/stats"
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

	http.HandleFunc("/metrics", exportMetrics(*enableDocker))

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

type cgroupV1Metrics struct {
	*v1.Metrics
	Process *ProcessStats `json:"process_stats"`
}

func statsCgroupsV1(ctx context.Context) (map[string]*cgroupV1Metrics, error) {
	if cgroups.Mode() == cgroups.Unified {
		return map[string]*cgroupV1Metrics{}, nil
	}
	system, err := cgroups.Load(subsystem, cgroups.StaticPath(*cgroupPath))
	if err != nil {
		log.Fatalf("cgroups load: %s", err)
	}

	processes, err := system.Processes(cgroups.Devices, true)
	if err != nil {
		return nil, fmt.Errorf("cgroups load: %s", err)
	}

	groups := make(map[string]*cgroupV1Metrics, len(processes))
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
		groups[name] = &cgroupV1Metrics{
			Metrics: stats,
			Process: ps,
		}
	}
	return groups, nil
}

func gatherAllDirs(basepath, path string) []string {
	dirs := []string{path}

	files, err := ioutil.ReadDir(basepath + "/" + path)

	if err != nil {
		log.Printf("gatherAllDirs ReadDir: %s", err)
		return dirs
	}

	for _, f := range files {
		if f.IsDir() {
			dirs = append(dirs, gatherAllDirs(basepath, path+"/"+f.Name())...)
		}
	}
	return dirs
}

func statsCgroupsV2(ctx context.Context) (map[string]*stats.Metrics, error) {
	if cgroups.Mode() != cgroups.Unified {
		return map[string]*stats.Metrics{}, nil
	}
	cgroupsMountpoint := "/sys/fs/cgroup"
	allCgNames := gatherAllDirs(cgroupsMountpoint, *cgroupPath)
	allCgNames = append(allCgNames, *cgroupPath)

	groups := make(map[string]*stats.Metrics, len(allCgNames))

	for _, cgName := range allCgNames {
		manager, err := v2.LoadManager(cgroupsMountpoint, cgName)
		if err != nil {
			log.Printf("cgroupsv2 load manager %s: %s", cgName, err)
			continue
		}
		stats, err := manager.Stat()
		if err != nil {
			log.Printf("cgroupsv2 stat %s: %s", cgName, err)
			continue
		}
		groups[cgName] = stats
	}
	return groups, nil
}

type dockerStats struct {
	CPU     types.CPUStats    `json:"cpu_stats,omitempty"`
	PreCPU  types.CPUStats    `json:"precpu_stats,omitempty"` // "Pre"="Previous"
	Memory  types.MemoryStats `json:"memory_stats,omitempty"`
	Process *ProcessStats     `json:"process_stats"`
}

func statsDockerContainers(ctx context.Context) (map[string]dockerStats, error) {
	dockerClient, err := client.NewEnvClient()
	if err != nil {
		log.Fatalf("%v", err)
	}
	defer dockerClient.Close()

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

func exportMetrics(enableDocker bool) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		groupsV1, err := statsCgroupsV1(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		groupsV2, err := statsCgroupsV2(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var dockerContainers = make(map[string]dockerStats)
		if enableDocker {
			dockerContainers, err = statsDockerContainers(ctx)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		fmt.Fprintln(w, `# HELP container_cpu_user_seconds_total Cumulative user cpu time consumed in seconds.
# TYPE container_cpu_user_seconds_total counter`)
		for name, stats := range groupsV1 {
			fmt.Fprintf(w, `container_cpu_user_seconds_total{id=%s} %.2f`, strconv.Quote(name), float64(stats.CPU.Usage.User)/1000000000.0)
			fmt.Fprintln(w)
		}
		for name, stats := range groupsV2 {
			fmt.Fprintf(w, `container_cpu_user_seconds_total{id=%s} %.2f`, strconv.Quote(name), float64(stats.CPU.UserUsec)/1000000.0)
			fmt.Fprintln(w)
		}
		for name, stats := range dockerContainers {
			fmt.Fprintf(w, `container_cpu_user_seconds_total{id=%s} %.2f`, strconv.Quote(name), float64(stats.CPU.CPUUsage.UsageInUsermode)/1000000000.0)
			fmt.Fprintln(w)
		}

		fmt.Fprintln(w, `# HELP container_cpu_seconds_total Cumulative cpu time consumed in seconds.
# TYPE container_cpu_seconds_total counter`)
		for name, stats := range groupsV1 {
			fmt.Fprintf(w, `container_cpu_seconds_total{id=%s} %.2f`, strconv.Quote(name), float64(stats.CPU.Usage.Total)/1000000000.0)
			fmt.Fprintln(w)
		}
		for name, stats := range groupsV2 {
			fmt.Fprintf(w, `container_cpu_seconds_total{id=%s} %.2f`, strconv.Quote(name), float64(stats.CPU.UsageUsec)/1000000.0)
			fmt.Fprintln(w)
		}
		for name, stats := range dockerContainers {
			fmt.Fprintf(w, `container_cpu_seconds_total{id=%s} %.2f`, strconv.Quote(name), float64(stats.CPU.CPUUsage.TotalUsage)/1000000000.0)
			fmt.Fprintln(w)
		}

		fmt.Fprintln(w, `# HELP container_memory_usage_bytes Current memory usage in bytes, including all memory regardless of when it was accessed
# TYPE container_memory_usage_bytes gauge`)
		for name, stats := range groupsV1 {
			fmt.Fprintf(w, `container_memory_usage_bytes{id=%s} %d`, strconv.Quote(name), stats.Memory.Usage.Usage)
			fmt.Fprintln(w)
		}
		for name, stats := range groupsV2 {
			fmt.Fprintf(w, `container_memory_usage_bytes{id=%s} %d`, strconv.Quote(name), stats.Memory.Usage)
			fmt.Fprintln(w)
		}
		for name, stats := range dockerContainers {
			fmt.Fprintf(w, `container_memory_usage_bytes{id=%s} %d`, strconv.Quote(name), stats.Memory.Usage)
			fmt.Fprintln(w)
		}

		fmt.Fprintln(w, `# HELP container_memsw_usage_bytes Current memory+swap usage in bytes
# TYPE container_memsw_usage_bytes gauge`)
		for name, stats := range groupsV1 {
			fmt.Fprintf(w, `container_memsw_usage_bytes{id=%s} %d`, strconv.Quote(name), stats.Memory.Swap.Usage)
			fmt.Fprintln(w)
		}
		for name, stats := range groupsV2 {
			fmt.Fprintf(w, `container_memsw_usage_bytes{id=%s} %d`, strconv.Quote(name), stats.Memory.Usage+stats.Memory.SwapUsage)
			fmt.Fprintln(w)
		}

		fmt.Fprintln(w, `# HELP container_memory_rss Size of RSS in bytes.
# TYPE container_memory_rss gauge`)
		for name, stats := range groupsV1 {
			fmt.Fprintf(w, `container_memory_rss{id=%s} %d`, strconv.Quote(name), stats.Memory.RSS)
			fmt.Fprintln(w)
		}
		for name, stats := range dockerContainers {
			fmt.Fprintf(w, `container_memory_rss{id=%s} %d`, strconv.Quote(name), stats.Memory.Stats["rss"])
			fmt.Fprintln(w)
		}

		fmt.Fprintln(w, `# HELP container_open_fds Number of open file descriptors
# TYPE container_open_fds gauge`)
		for name, stats := range groupsV1 {
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
		for name, stats := range groupsV1 {
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
