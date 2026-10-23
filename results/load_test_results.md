# ChatScale Load Test Results

Phase 6 measurements, all run locally (AWS is deferred). Every scenario was run three times; the tables show each run and the median. Raw data: `results/runs.jsonl` (one JSON line per run, with a 5-second timeline), `results/log-*.txt` (console output), `results/prometheus/*.json` (server-side series for each test window) and `results/grafana/*.png` (dashboard screenshots). `go run ./cmd/loadsummary results/runs.jsonl` regenerates the tables below.

## Resume numbers

| Figure | Value | Source |
| --- | --- | --- |
| **N**: concurrent viewers sustained | **9,000** across 3 nodes (1.8M message deliveries/sec) | median of the last stable level in 3 ceiling runs (9,000 / 9,000 / 9,000) |
| **X**: p99 end-to-end delivery latency | **62 ms** median (58.7 / 61.9 / 69.2 ms across the runs, so under 70 ms in all three) | the same level's own samples |

> Built a distributed live chat service in Go (goroutines, channels, WebSockets, Redis Pub/Sub) that sustained **9,000** concurrent viewers at p99 delivery latency under **70 ms** across 3 nodes

Read these as **local** numbers: an 18-core Apple M5 Pro laptop with the gateways, Redis, Prometheus and the load generator all sharing one Docker VM (see Environment). They are not t3.medium figures, and nothing here was run on AWS. Be ready to say that in an interview; the numbers are real, but the machine is not a cloud instance.

## Environment

| | |
| --- | --- |
| Host | Apple M5 Pro, 18 cores, 48 GB RAM, macOS |
| Docker Desktop VM | 18 CPUs, 7.7 GiB RAM; **everything below shares it** |
| Gateways | 3 × the Go gateway container, no CPU or memory limits |
| Redis | 1 × redis:7-alpine, Pub/Sub only |
| Load generator | `cmd/loadgen` as a container on the compose network, talking to `gateway-N:8080` directly |
| Gateway rate limits | raised to capacity 1000 / refill 1000 per user for load tests (defaults of 5 / 2 per second would reject the senders) |
| Workload | **one room**; every connection receives every message, so deliveries/sec = connections × senders × msg-rate. This is the hot-room worst case for fan-out |
| Between runs | gateways recreated (clears connections, rate-limit state and counters), then 15 s for Prometheus to scrape the new processes |

## How to read the figures

- **End-to-end latency** is measured by the load generator: `receivedAt - sentAtNano`, in one process on one clock. It includes the gateway, Redis, the Docker bridge network and the load generator's own read scheduling. Percentiles come from a log-linear histogram (about 3% resolution), because at hundreds of thousands of deliveries per second keeping every sample is impossible.
- **Fan-out latency** is the server-side `chatscale_fanout_latency_seconds`: time from the Redis subscriber receiving a message to the last local client's channel send. It excludes everything after the channel. The two numbers answer different questions and both are reported.
- **Server drops** are `chatscale_messages_dropped_total`: messages dropped because a client's send buffer was full. They are measured directly and were **zero in every run**.
- **Est. loss %** is derived: deliveries owed (connected clients at each send) minus deliveries received. It is a noisy estimate (about ±0.05%, and 0.17% in one baseline run), most likely from connections counting as ready slightly before the gateway has registered them. It does not count clients that were disconnected at the time as owed anything.
- **Ceiling search.** Each ramp step (+1,000 connections every 30 s) is a *level*, judged only on samples taken inside it, from its halfway point on. A trailing-minute figure would still contain the previous, lighter level just after a step and flatter the new one. A level is *stable* only if it finishes within bounds.

## Results

### baseline

| run | peak conns | msgs sent | est. loss % | p50 ms | p95 ms | p99 ms | server drops | fan-out p99 ms | max GC pause ms | peak RSS MiB (max node) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 1000 | 594 | 0.000 | 5.5 | 8.0 | 10.5 | 0 | 1.466 | 1.25 | 35 |
| 2 | 1000 | 595 | 0.168 | 5.0 | 7.6 | 11.0 | 0 | 0.977 | 0.98 | 37 |
| 3 | 1000 | 595 | 0.000 | 5.6 | 8.3 | 11.0 | 0 | 0.978 | 0.63 | 37 |
| **median** | **1000** | **595** | **0.000** | **5.5** | **8.0** | **11.0** | **0** | **0.978** | **0.98** | **37** |

### medium

| run | peak conns | msgs sent | est. loss % | p50 ms | p95 ms | p99 ms | server drops | fan-out p99 ms | max GC pause ms | peak RSS MiB (max node) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 3000 | 5990 | 0.016 | 3.3 | 7.1 | 10.0 | 0 | 4.061 | 3.05 | 65 |
| 2 | 3000 | 5990 | 0.000 | 3.7 | 9.4 | 12.1 | 0 | 4.127 | 1.82 | 67 |
| 3 | 3000 | 5989 | 0.000 | 3.5 | 7.6 | 10.5 | 0 | 4.249 | 2.65 | 66 |
| **median** | **3000** | **5990** | **0.000** | **3.5** | **7.6** | **10.5** | **0** | **4.127** | **2.65** | **66** |

### high

| run | peak conns | msgs sent | est. loss % | p50 ms | p95 ms | p99 ms | server drops | fan-out p99 ms | max GC pause ms | peak RSS MiB (max node) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 5000 | 23979 | 0.009 | 6.4 | 14.7 | 23.1 | 0 | 5.860 | 15.63 | 88 |
| 2 | 5000 | 23979 | -0.002 | 7.2 | 18.4 | 24.6 | 0 | 6.302 | 6.70 | 90 |
| 3 | 5000 | 23980 | 0.004 | 7.5 | 20.4 | 29.4 | 0 | 7.348 | 8.19 | 91 |
| **median** | **5000** | **23979** | **0.004** | **7.2** | **18.4** | **24.6** | **0** | **6.302** | **8.19** | **90** |

### ceiling

| run | stable conns | stable p99 ms | peak conns | msgs sent | est. loss % | p50 ms | p95 ms | p99 ms | server drops | fan-out p99 ms | max GC pause ms | peak RSS MiB (max node) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 9000 | 61.9 | 10000 | 57995 | 0.049 | 11.5 | 48.2 | 79.7 | 0 | 22.496 | 28.19 | 124 |
| 2 | 9000 | 58.7 | 10000 | 57989 | 0.052 | 12.6 | 51.4 | 107.0 | 0 | 23.069 | 15.23 | 130 |
| 3 | 9000 | 69.2 | 10000 | 57991 | 0.040 | 12.1 | 49.3 | 155.2 | 0 | 22.456 | 25.98 | 126 |
| **median** | **9000** | **61.9** | **10000** | **57991** | **0.049** | **12.1** | **49.3** | **107.0** | **0** | **22.496** | **25.98** | **126** |

### failure

| run | peak conns | msgs sent | est. loss % | p50 ms | p95 ms | p99 ms | server drops | fan-out p99 ms | max GC pause ms | peak RSS MiB (max node) | reconnects | reconnect p50 ms | reconnect p95 ms | reconnect max ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 9000 | 23925 | 5.350 | 27.3 | 1577.1 | 1778.4 | 0 | 24.010 | 12.21 | 193 | 3000 | 1068 | 9782 | 10901 |
| 2 | 9000 | 23908 | 5.660 | 27.3 | 1610.6 | 1711.3 | 0 | 24.250 | 11.09 | 191 | 3000 | 1024 | 6189 | 7351 |
| 3 | 9000 | 23895 | 4.530 | 28.8 | 1577.1 | 1677.7 | 0 | 24.301 | 8.93 | 190 | 3000 | 1031 | 2937 | 4085 |
| **median** | **9000** | **23908** | **5.350** | **27.3** | **1577.1** | **1711.3** | **0** | **24.250** | **11.09** | **191** | **3000** | **1031** | **6189** | **7351** |

### failure5k

| run | peak conns | msgs sent | est. loss % | p50 ms | p95 ms | p99 ms | server drops | fan-out p99 ms | max GC pause ms | peak RSS MiB (max node) | reconnects | reconnect p50 ms | reconnect p95 ms | reconnect max ms |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 5000 | 23909 | 0.002 | 6.3 | 16.0 | 22.0 | 0 | 6.560 | 2.68 | 120 | 1667 | 1026 | 1461 | 1522 |
| 2 | 5000 | 23912 | 0.005 | 5.6 | 12.3 | 17.3 | 0 | 6.656 | 6.53 | 126 | 1667 | 1003 | 1457 | 1518 |
| 3 | 5000 | 23903 | 0.020 | 8.0 | 18.9 | 25.7 | 0 | 8.134 | 5.96 | 124 | 1667 | 1014 | 1475 | 1524 |
| **median** | **5000** | **23909** | **0.005** | **6.3** | **16.0** | **22.0** | **0** | **6.656** | **5.96** | **124** | **1667** | **1014** | **1461** | **1522** |

### spike

| run | peak conns | msgs sent | est. loss % | p50 ms | p95 ms | p99 ms | server drops | fan-out p99 ms | max GC pause ms | peak RSS MiB (max node) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 5000 | 23979 | 0.002 | 7.1 | 18.9 | 24.6 | 0 | 7.131 | 4.31 | 91 |
| 2 | 5000 | 23980 | 0.004 | 5.2 | 13.9 | 17.8 | 0 | 4.911 | 7.64 | 86 |
| 3 | 5000 | 23982 | 0.024 | 6.9 | 14.2 | 18.9 | 0 | 5.591 | 9.59 | 89 |
| **median** | **5000** | **23980** | **0.004** | **6.9** | **14.2** | **18.9** | **0** | **5.591** | **7.64** | **89** |


### Ceiling search, level by level

Each level's p99 over its own samples. The search stops at the first level whose p99 exceeds 100 ms (the blueprint's High-load bound).

| connections | deliveries/s | run 1 p99 ms | run 2 p99 ms | run 3 p99 ms |
| ---: | ---: | ---: | ---: | ---: |
| 1,000 | 0.2M | 8.0 | 6.9 | 9.4 |
| 2,000 | 0.4M | 6.8 | 5.9 | 8.9 |
| 3,000 | 0.6M | 12.6 | 11.0 | 11.5 |
| 4,000 | 0.8M | 12.1 | 13.9 | 20.4 |
| 5,000 | 1.0M | 15.5 | 23.1 | 21.0 |
| 6,000 | 1.2M | 25.7 | 25.7 | 28.8 |
| 7,000 | 1.4M | 34.6 | 34.6 | 31.5 |
| 8,000 | 1.6M | 44.0 | 44.0 | 44.0 |
| 9,000 | 1.8M | 61.9 | 58.7 | 69.2 |
| 10,000 | 2.0M | 176.2 | 310.4 | 419.4 |

## Against the blueprint's success criteria

| Scenario | Criterion | Result |
| --- | --- | --- |
| Baseline (1,000 conns) | drops = 0; p99 < 20 ms | **met**: 0 drops, p99 11.0 ms |
| Medium (3,000 conns) | drops = 0; p99 < 50 ms | **met**: 0 drops, p99 10.5 ms |
| High (5,000 conns, 1M deliveries/s) | drops < 5/min; p99 < 100 ms | **met**: 0 drops, p99 24.6 ms |
| Ceiling | peak N at threshold | **9,000** stable; first failing level 10,000 (p99 176 to 419 ms). The blueprint's drop threshold never fired (see below) |
| Node failure, at the ceiling (9,000 conns) | reconnect < 5 s; no crash | reconnect p50 1.0 s, p95 6.2 s, max 7.4 s: **not met**. No surviving node crashed or restarted |
| Node failure, with headroom (5,000 conns) | reconnect < 5 s; no crash | reconnect p50 1.0 s, p95 1.5 s, max 1.5 s: **met**. No surviving node crashed or restarted |
| Spike (500 → 5,000 in 5 s) | no OOM; GC pause < 100 ms | **met**: no OOM, max GC pause 7.6 ms (worst single run 9.6 ms), p99 18.9 ms |

## What the numbers say

**The ceiling is the machine's CPU, not memory or the design's buffers.** At 11,000 connections a probe showed each gateway using about 4.9 cores and the load generator 3.7, together about 18.4 cores, the whole Docker VM. Throughput plateaus near 2M deliveries/sec; beyond that, queues grow and latency climbs into seconds. Memory was never close to a limit: peak RSS about 126 MiB per gateway at 10,000 connections (roughly 32 KiB per connection above the Go runtime's baseline), against 7.7 GiB available. Redis stayed at 1 to 3% of a core.

**The blueprint's "drops > 5/min" stop never fires here.** At this load the system degrades by queueing: sockets and kernel buffers absorb the backlog and latency grows, while the 256-slot per-client channels never fill. An uncapped first attempt ramped to 40,000 connections with zero drops while p99 reached about 9 seconds and the load generator received only 22% of the deliveries it expected (`results/notes/ceiling-drops-criterion-only.*`). So the search also stops when a level's p99 exceeds 100 ms. The drop threshold is still checked.

**Load generator placement changed the result by 20x.** Run from macOS through Docker Desktop's port forwarding, the same 3,000-connection load gave p99 277 ms (`results/notes/medium-via-host-port-forwarding.txt`). Inside the Docker network it gave 10 ms with zero loss. The port forwarder, not the gateways, was the bottleneck, so the load generator runs as a container.

**Node failure: recovery is fast when there is headroom, and a node loss at the limit is an overload.** At 5,000 connections, killing gateway-2 at T = 60 s disconnected 1,667 clients; all reconnected to the surviving nodes with p50 1.0 s and max 1.5 s, which is essentially the client's own backoff (500 ms base plus up to 1 s jitter). Total delivery stayed near 1M/s and p99 stayed near 22 ms. At 9,000 connections the same event moved 3,000 clients onto two nodes that cannot carry 1.8M deliveries/sec: all clients were back within about 5 s, but p99 then sat near 1.6 s and the estimated loss reached 5%, so reconnect times (p95 up to 9.8 s) include queueing. That is capacity, not a recovery bug.

**Message loss window.** Redis Pub/Sub is at-most-once, so messages published while a client is disconnected are gone for it. The load generator's loss figure does not count those (a disconnected client is not "owed" anything), so I estimate it separately for the 5,000-connection failure: 1,667 clients offline for about 1 s each at 200 msg/s is roughly 330,000 missed deliveries, about 0.3% of the ~120M delivered in the run. That is an estimate from reconnect durations, not a direct count.

## Limits of these measurements

- One machine, one Docker VM, load generator and gateways competing for the same CPUs. A separate load-generator host would raise the ceiling and lower latency.
- One room, one message rate per scenario, synthetic traffic: all viewers receive all messages. Real chat spreads viewers over many rooms.
- Latency percentiles carry about 3% histogram resolution. The ceiling's 9,000 versus 10,000 boundary is a ramp step of 1,000, so N is accurate to one step.
- `est. loss` is derived and noisy as described above; the server-side drop counter is the authoritative figure.
- Not run: DevTools heap-snapshot leak checks of the React client, and the mobile-on-LAN check from Phase 4.
