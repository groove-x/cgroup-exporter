package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/containerd/cgroups"
)

var (
	versionFlag = flag.Bool("version", false, "version")
	version     string
	git         string

	address = flag.String("address", ":48900", "address")
)

func main() {
	flag.Parse()
	if *versionFlag {
		fmt.Printf("version %s, git %s\n", version, git)
		return
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

	system, err := cgroups.Load(subsystem, cgroups.StaticPath("/system.slice"))
	if err != nil {
		log.Fatalf("cgroups load: %s", err)
	}

	http.HandleFunc("/metrics", exportMetrics(system))

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

var (
	cpuUserTotalHeader = `# HELP container_cpu_user_seconds_total Cumulative user cpu time consumed in seconds.
# TYPE container_cpu_user_seconds_total counter
`
	cpuUserTotalFormat = `container_cpu_user_seconds_total{id="%s"} %.2f
`

	memoryUsageHeader = `# HELP container_memory_usage_bytes Current memory usage in bytes.
# TYPE container_memory_usage_bytes gauge
`
	memoryUsageFormat = `container_memory_usage_bytes{id="%s"} %d
`
)

func subsystem() ([]cgroups.Subsystem, error) {
	root := "/sys/fs/cgroup"
	s := []cgroups.Subsystem{
		cgroups.NewDevices(root),
		cgroups.NewCpuacct(root),
		cgroups.NewMemory(root),
	}
	return s, nil
}

func exportMetrics(system cgroups.Cgroup) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		processes, err := system.Processes(cgroups.Devices, true)
		if err != nil {
			msg := fmt.Sprintf("cgroups load: %s", err)
			http.Error(w, msg, http.StatusInternalServerError)
		}

		groups := make(map[string]*cgroups.Metrics, len(processes))
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
			groups[name] = stats
		}

		fmt.Fprint(w, cpuUserTotalHeader)
		for name, stats := range groups {
			fmt.Fprintf(w, cpuUserTotalFormat, name, float64(stats.CPU.Usage.User)/1000000000.0)
		}
		fmt.Fprint(w, memoryUsageHeader)
		for name, stats := range groups {
			fmt.Fprintf(w, memoryUsageFormat, name, stats.Memory.Usage.Usage)
		}
		return
	}
}
