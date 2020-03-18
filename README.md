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
:

# HELP container_memory_usage_bytes Current memory usage in bytes, including all memory regardless of when it was accessed
# TYPE container_memory_usage_bytes gauge
container_memory_usage_bytes{id="/system.slice/wpa_supplicant.service"} 1871872
container_memory_usage_bytes{id="/system.slice/ssh.service"} 61440
container_memory_usage_bytes{id="/system.slice/docker.service"} 37171200
container_memory_usage_bytes{id="/system.slice/NetworkManager.service"} 18305024
:

# HELP container_memory_rss Size of RSS in bytes.
# TYPE container_memory_rss gauge
container_memory_rss{id="/system.slice/ssh.service"} 110592
container_memory_rss{id="/system.slice/NetworkManager.service"} 5066752
container_memory_rss{id="/system.slice/wpa_supplicant.service"} 331776
container_memory_rss{id="/system.slice/docker.service"} 24072192
:
```
