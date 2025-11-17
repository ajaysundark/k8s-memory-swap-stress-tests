# KubeCon NA 2025: Deep Dive: Handling Kubernetes Memory Pressure & Achieving Workload Stability With NodeSwap

This repository contains the slides, stress test artifacts, and analysis data for the KubeCon North America 2025 presentation on Kubernetes NodeSwap.

---

## Session Summary

**The Problem:** Effectively managing memory pressure is key to stable Kubernetes workloads and preventing OOM errors. The Kubernetes **NodeSwap** feature, GA from **v1.34**, promises better resource management and node stability.

**What We Cover:** This talk shares first-hand insights and practical learnings from rigorous stress testing done at Google, focusing on the trade-offs, kernel performance tuning, and the current limitations in eviction logic and observability when using swap in production clusters. We also discuss the future of pod-level swap control.

---

## Key Resources

| Resource | Description | Link |
| :--- | :--- | :--- |
| **Presentation Slides** | The full, embedded slide deck from the session. | [View Slides on GitHub Pages](https://ajaysundark.github.io/k8s-memory-swap-stress-tests/) |
| **Stress Test Code** | Kubernetes manifests, configurations, and scripts used for the NodeSwap stress tests. | [Explore Code Artifacts](./stress-tests-code/) |
| **KubeCon Sched** | Official session details, abstract, and speaker information. | [View on Sched.com](https://kccncna2025.sched.com/event/dd1483bb74f02af3c69fc761310a5a6f/edit) |


### Kernel References

#### Default Linux Kernel Memory Buffer Calculation

```
// From a 1.32 GKE node -- 8GB ubuntu 
$ sysctl vm.min_free_kbytes
vm.min_free_kbytes = 67584 // ~68MB
```

Linux kernel will set this value based on RAM size and thp pagesizes - a good explanation of how it gets [68MB in x86 systems is here](https://unix.stackexchange.com/a/525457).

From kernel bits --
1. Initial kernel calculation (lowmem bytes) -- setting default `linux/mm /page_alloc.c:calculate_min_free_kbytes` [here](https://github.com/torvalds/linux/blob/master/mm/page_alloc.c#L6204).
2. Huge-table adjustment [here](https://elixir.bootlin.com/linux/v5.0.17/source/mm/khugepaged.c#L1862)   

---

## Connect

* **Kubernetes Slack:** ajaysundark@ `#sig-node` channel
