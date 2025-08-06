# cgroup-exporter 

[![wercker status](https://app.wercker.com/status/c5dbed66eb7477a1c3a0b1a8bfe879c0/s/master "wercker status")](https://app.wercker.com/project/byKey/c5dbed66eb7477a1c3a0b1a8bfe879c0) [![Go Report Card](https://goreportcard.com/badge/github.com/groove-x/cgroup-exporter)](https://goreportcard.com/report/github.com/groove-x/cgroup-exporter)

simple cgroup cpu,memory exporter

## releases

https://github.com/groove-x/cgroup-exporter/releases

## example

`curl localhost:48900/metrics`

```
# HELP container_cpu_user_seconds_total Cumulative user cpu time consumed in seconds.
# TYPE container_cpu_user_seconds_total counter
container_cpu_user_seconds_total{id="/system.slice/wpa_supplicant.service"} 524.87
container_cpu_user_seconds_total{id="/system.slice/ssh.service"} 1.30
container_cpu_user_seconds_total{id="/system.slice/docker.service"} 2219.16
container_cpu_user_seconds_total{id="/system.slice/NetworkManager.service"} 4283.36
container_cpu_user_seconds_total{id="/docker/grafana"} 0.70
:

# HELP container_memory_usage_bytes Current memory usage in bytes, including all memory regardless of when it was accessed
# TYPE container_memory_usage_bytes gauge
container_memory_usage_bytes{id="/system.slice/wpa_supplicant.service"} 1871872
container_memory_usage_bytes{id="/system.slice/ssh.service"} 61440
container_memory_usage_bytes{id="/system.slice/docker.service"} 37171200
container_memory_usage_bytes{id="/system.slice/NetworkManager.service"} 18305024
container_memory_usage_bytes{id="/docker/grafana"} 61407232
:

# HELP container_memory_rss Size of RSS in bytes.
# TYPE container_memory_rss gauge
container_memory_rss{id="/system.slice/wpa_supplicant.service"} 331776
container_memory_rss{id="/system.slice/ssh.service"} 110592
container_memory_rss{id="/system.slice/docker.service"} 24072192
container_memory_rss{id="/system.slice/NetworkManager.service"} 5066752
container_memory_rss{id="/docker/grafana"} 16224256
:

# HELP container_open_fds Number of open file descriptors
# TYPE container_open_fds gauge
container_open_fds{id="/system.slice/wpa_supplicant.service"} 19
container_open_fds{id="/system.slice/ssh.service"} 5
container_open_fds{id="/system.slice/docker.service"} 29
container_open_fds{id="/system.slice/NetworkManager.service"} 21
container_open_fds{id="/docker/grafana"} 11
:

# HELP container_open_sockets Number of open sockets
# TYPE container_open_sockets gauge
container_open_sockets{id="/system.slice/wpa_supplicant.service"} 16
container_open_sockets{id="/system.slice/ssh.service"} 4
container_open_sockets{id="/system.slice/docker.service"} 19
container_open_sockets{id="/system.slice/NetworkManager.service"} 13
container_open_sockets{id="/docker/grafana"} 3
:
```

## options

| arg | description |
| --- | --- |
| `--metrics.docker` | enable docker container metrics |
| `--cgroup-version` | cgroup version to use (v1, v2) |

## customize systemd

You can customize systemd setting with options.

`/etc/systemd/system/prometheus-cgroup-exporter.service.d/local.conf`:

```
[Service]
ExecStart=
ExecStart=/usr/local/bin/cgroup-exporter --metrics.docker
```

see [systemd.unit / Example 2. Overriding vendor settings](https://www.freedesktop.org/software/systemd/man/systemd.unit.html#id-1.14.3).
