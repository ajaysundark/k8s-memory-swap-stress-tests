# Swap Stress Test Suite

This repository contains a suite of tools to stress test memory and swap usage
on a Kubernetes node. It is designed to simulate various memory pressure
scenarios and collect detailed metrics for analysis.

## Overview

The test suite consists of two main components:

1.  **`simple-swap-test`**: A Go application that acts as the core stressor. It
    allocates memory in configurable increments, can create file-cache pressure,
    and supports different "hold" strategies to simulate various workload
    patterns (`hot` random access, `sequential` access, or `sleep`).
2.  **`vmstat-sidecar`**: A sidecar container that runs `vmstat` alongside the
    main test to capture detailed system-level statistics (like memory, swap,
    and CPU usage) throughout the test run.

The test is orchestrated using the provided `k8s-simple-swap-test.yml`
Kubernetes manifest.

## Prerequisites

*   A Kubernetes cluster with swap enabled on the worker nodes.
*   `docker` installed locally to build the container image.
*   `kubectl` configured to communicate with your cluster.
*   Access to a container registry to host your test image.

## Building the Test Image

The stress test runs as a container. You need to build the image and push it to
a registry accessible by your Kubernetes cluster.

1.  **Build the image:** `docker build --no-cache -t
    gcr.io/ajaysundar-gke-dev/simple_swap_test:latest .`

2.  **Push the image:** `docker push gcr.io/ajaysundar-gke-dev/simple_swap_test:latest`

## Configuration

### Kubernetes Manifest (`k8s-simple-swap-test.yml`)

Before running the test, you may want to configure the Kubernetes manifest:

*   **Image**: Update the `spec.containers.image` field to point to the image
    you just pushed.
*   **`log-prefix`**: The `metadata.labels.log-prefix` is used to create a
    unique directory on the host node for storing reports and logs. This helps
    in organizing results from multiple runs.
*   **`args`**: The command-line arguments for the `swap_stress_test` are
    configured here. This is where you define the parameters for your specific
    test run.
*   **`nodeSelector`**: Ensure this points to a node where you want to run the
    test.

### Stress Test Flags

The Go application accepts the following flags, which you can set in the `args`
section of the YAML file:

*   `--targetMiB`: Total memory to allocate in MiB.
*   `--incrementMiB`: Amount of memory to allocate in each step (in MiB).
*   `--timeout`: Delay between each memory allocation increment in milliseconds.
*   `--hold`: Duration in minutes to hold the memory after allocation.
*   `--holdStrategy`: Defines the activity during the hold period.
    *   `hot`: Actively touch random memory locations to simulate a hot cache.
    *   `sequential`: Actively touch memory in a sequential order.
    *   `sleep`: Do nothing and just hold the memory.
*   `--fileCacheMiB`: Target size of file-backed cache to create for testing
    page cache pressure.
*   `--report`: The directory path where the JSON metrics report will be saved.
    This is configured to use the `log-prefix` label.

## Running the Test

1.  **Deploy the test to your cluster:** `bash kubectl apply -f
    k8s-simple-swap-test.yml`

2.  **Monitor the test:** You can monitor the progress by checking the logs of
    the containers.

    ```bash
    # Check logs of the main stress test
    kubectl logs simple-swap-test -c stress-container -f

    # Check logs of the vmstat sidecar
    kubectl logs simple-swap-test -c vmstat-sidecar -f
    ```

## Collecting Results

The test suite generates two primary artifacts, which are saved to a directory
on the host node's `/tmp` volume (e.g., `/tmp/simple-swap-test-run/`):

1.  **JSON Report**: A detailed JSON file (`..._report_....json`) containing the
    metrics collected by the `simple-swap-test` application. This report is
    updated periodically, so you can get data even if the test fails mid-run.
2.  **`vmstat` Log**: A log file (`..._vmstat.log`) containing the output from
    the `vmstat` sidecar, providing a second-by-second snapshot of the system's
    state.

You will need to access the node's filesystem to retrieve these files.

One option is to use a node debugging pod: `kubectl debug node/node-name -it
--image alpine`

With these pods running, you could export the log file to x20 using kubectl cp.
eg: `kubectl cp
default/pod-name:/host/tmp/simple-swap-test-300MBps-rate-12GiB-target_vmstat.log
~/simple-swap-test/experiments/data`

## Cleanup

Once the test is complete, you can delete the pod:

```bash
kubectl delete pod node-debugger-pod
kubectl delete -f k8s-simple-swap-test.yml
```
