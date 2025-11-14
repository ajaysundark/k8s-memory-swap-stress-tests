// simple-swap-test is a simple memory stressor to test swap usage.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"golang.org/x/exp/mmap"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type freeBytes struct {
	total     uint64
	used      uint64
	free      uint64
	buffCache uint64
}

// TimePoint represents a single data point in time for memory and swap usage.
type TimePoint struct {
	Timestamp       time.Time `json:"timestamp"`
	PodMemoryUsage  uint64    `json:"podMemoryUsage"`
	PodSwapUsage    uint64    `json:"podSwapUsage"`
	SystemPageCache uint64    `json:"systemPageCache"`
	CPUAvg10Some    float64   `json:"cpuavg10some,omitempty"`
	IoAvg10Some     float64   `json:"ioavg10some,omitempty"`
	MemAvg10Some    float64   `json:"memavg10some,omitempty"`
	MemAvg10Full    float64   `json:"memavg10full,omitempty"`
	CPUTotalSome    uint64    `json:"cputotalsome,omitempty"`
	IoTotalSome     uint64    `json:"iototalsome,omitempty"`
	MemTotalSome    uint64    `json:"memtotalsome,omitempty"`
	MemTotalFull    uint64    `json:"memtotalfull,omitempty"`
}

// MemoryMetrics stores the metrics collected during the stress test
type MemoryMetrics struct {
	StartTime time.Time `json:"startTime"`
	EndTime   time.Time `json:"endTime"`
	// Initial system memory metrics
	InitialSystemTotalMemory uint64 `json:"initialSystemTotalMemory"`
	InitialSystemUsedMemory  uint64 `json:"initialSystemUsedMemory"`
	InitialSystemFreeMemory  uint64 `json:"initialSystemFreeMemory"`
	// Peak system memory metrics
	PeakSystemTotalMemory uint64 `json:"peakSystemMemory"`
	PeakSystemUsedMemory  uint64 `json:"peakSystemUsedMemory"`
	PeakSystemFreeMemory  uint64 `json:"peakSystemFreeMemory"`
	// Initial system swap metrics
	InitialSystemTotalSwap uint64 `json:"initialSystemTotalSwap"`
	InitialSystemUsedSwap  uint64 `json:"initialSystemUsedSwap"`
	InitialSystemFreeSwap  uint64 `json:"initialSystemFreeSwap"`
	// Peak system swap metrics
	PeakSystemTotalSwap uint64 `json:"peakSystemTotalSwap"`
	PeakSystemUsedSwap  uint64 `json:"peakSystemUsedSwap"`
	PeakSystemFreeSwap  uint64 `json:"peakSystemFreeSwap"`
	// Final swap metrics to determine if swap gets freed up after memory is released.
	FinalSystemTotalSwap uint64      `json:"finalSystemTotalSwap"`
	FinalSystemUsedSwap  uint64      `json:"finalSystemUsedSwap"`
	FinalSystemFreeSwap  uint64      `json:"finalSystemFreeSwap"`
	PodSwapLimit         uint64      `json:"podSwapLimit"` // TODO: Add pod swap limit
	PeakPodMemoryUsage   uint64      `json:"peakPodMemoryUsage"`
	PeakPodSwapUsage     uint64      `json:"peakPodSwapUsage"`
	FinalPodSwapUsage    uint64      `json:"finalPodSwapUsage"`
	GrowthMetrics        []TimePoint `json:"growthMetrics,omitempty"`
}

// StressConfig holds the configuration parameters for the stress test
type StressConfig struct {
	targetBytesMiB    uint64
	fileCacheBytesMiB uint64
	incrementBytesMiB uint64
	incrementTimeout  time.Duration
	holdDuration      time.Duration
	routineCount      int
	reportPath        string
	fileCachePath     string
	holdStrategy      string // "sleep", "hot" (random), or "sequential"
}

// FinalReport is the structure for the final JSON report.
type FinalReport struct {
	Config     ReportConfig  `json:"config"`
	Metrics    MemoryMetrics `json:"metrics"`
	SystemInfo struct {
		PodID        string `json:"podID"`
		QoSClass     string `json:"qosClass"`
		CgroupPath   string `json:"cgroupPath"`
		Swappiness   int    `json:"swappiness"`
		Kernel       string `json:"kernel"`
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
	} `json:"systemInfo"`
}

// ReportConfig is a subset of StressConfig with exported fields for final report
type ReportConfig struct {
	TargetBytesMiB    uint64        `json:"targetBytesMiB"`
	FileCacheBytesMiB uint64        `json:"fileCacheBytesMiB"`
	IncrementBytesMiB uint64        `json:"incrementBytesMiB"`
	IncrementTimeout  time.Duration `json:"incrementTimeout"`
	HoldDuration      time.Duration `json:"holdDuration"`
	RoutineCount      int           `json:"routineCount"`
	HoldStrategy      string        `json:"holdStrategy"`
}

// Global variables to store memory blocks to prevent garbage collection
var memoryBlocks [][]byte
var metrics MemoryMetrics
var metricsMutex sync.Mutex

var cgroupPath string

func main() {
	targetMiB := flag.Uint64("targetMiB", 1024, "Target memory to allocate in MiB")
	fileCacheMiB := flag.Uint64("fileCacheMiB", 0, "Target file cache to create in MiB")
	incrementMiB := flag.Uint64("incrementMiB", 100, "Increment memory to allocate in MiB")
	incrementTimeout := flag.Int("timeout", 10, "Timeout between increments in milliseconds")
	holdDuration := flag.Int("hold", 3, "Maximum duration to hold memory allocations in Minutes")
	routineCount := flag.Int("routines", 1, "Number of parallel routines to run")
	reportPath := flag.String("report", "/tmp/", "Report path to save metrics")
	fileCachePath := flag.String("fileCachePath", "/tmp/", "Path to store temporary files for file cache testing.")
	holdStrategy := flag.String("holdStrategy", "hot", "Hold strategy: 'sleep', 'hot' (random access), or 'sequential'.")

	flag.Parse()

	if *targetMiB == 0 {
		fmt.Printf(" --targetMiB must be specified.\n")
		os.Exit(1)
	}

	config := StressConfig{
		targetBytesMiB:    *targetMiB,
		fileCacheBytesMiB: *fileCacheMiB,
		incrementBytesMiB: *incrementMiB,
		incrementTimeout:  time.Duration(*incrementTimeout) * time.Millisecond,
		holdDuration:      time.Duration(*holdDuration) * time.Minute,
		routineCount:      *routineCount,
		reportPath:        *reportPath,
		fileCachePath:     *fileCachePath,
		holdStrategy:      *holdStrategy,
	}
	cgroupPath = getPodCgroupPath()
	metrics = MemoryMetrics{
		StartTime: time.Now(),
	}
	// Initial 'free' metrics
	m, s, err := getSystemMemory()
	if err != nil {
		fmt.Printf("Failed to get system memory: %v\n", err)
	}
	metrics.InitialSystemTotalMemory = m.total
	metrics.InitialSystemUsedMemory = m.used
	metrics.InitialSystemFreeMemory = m.free
	metrics.InitialSystemTotalSwap = s.total
	metrics.InitialSystemUsedSwap = s.used
	metrics.InitialSystemFreeSwap = s.free

	runStressTest(config)
	// Final 'free' metrics
	_, s, err = getSystemMemory()
	if err != nil {
		fmt.Printf("Failed to get system memory: %v\n", err)
	}
	metrics.FinalSystemTotalSwap = s.total
	metrics.FinalSystemUsedSwap = s.used
	metrics.FinalSystemFreeSwap = s.free
	reportMetrics(config)
}

// stress test runner
func runStressTest(config StressConfig) {
	var wg sync.WaitGroup

	targetBytes := config.targetBytesMiB * 1024 * 1024
	bytesPerRoutine := targetBytes / uint64(config.routineCount)
	fileCacheBytesPerRoutine := (config.fileCacheBytesMiB * 1024 * 1024) / uint64(config.routineCount)

	fmt.Printf("Starting stress test with %d routines.\n", config.routineCount)
	fmt.Printf("Swappiness: %d\n", getSwappiness())
	fmt.Printf("Target memory per routine: %d bytes.\n", bytesPerRoutine)
	if fileCacheBytesPerRoutine > 0 {
		fmt.Printf("Target file cache per routine: %d bytes.\n", fileCacheBytesPerRoutine)
	}

	// Start multiple stress routines
	for i := 0; i < config.routineCount; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			stressRoutine(routineID, bytesPerRoutine, fileCacheBytesPerRoutine, config)
		}(i)
	}

	// Wait for all routines to complete
	wg.Wait()

	metrics.EndTime = time.Now()
}

func stressRoutine(routineID int, targetBytes uint64, fileCacheTargetBytes uint64, config StressConfig) {
	// Create file-backed cache to exercise page cache
	var tempFiles []*os.File
	if fileCacheTargetBytes > 0 {
		fmt.Printf("Routine %d: Creating file cache of %d bytes.\n", routineID, fileCacheTargetBytes)
		var err error
		tempFiles, err = createAndPopulateFiles(routineID, fileCacheTargetBytes, config)
		if err != nil {
			fmt.Printf("Routine %d: Failed to create file cache: %v\n", routineID, err)
		}
		defer func() {
			for _, f := range tempFiles {
				f.Close()
				os.Remove(f.Name())
			}
		}()
	}

	// Stress Memory allocation strategy: fixed / gradual
	// Step up: start allocating memory
	allocationStart := time.Now()
	var allocatedBytes uint64
	var localBlocks [][]byte

	fmt.Printf("Routine %d: Starting memory allocation.\n", routineID)

	incrementBytes := config.incrementBytesMiB * 1024 * 1024
	for allocatedBytes < targetBytes {
		// Allocate memory block
		block := make([]byte, incrementBytes)
		// Write to memory to ensure it's actually allocated
		for i := range block {
			block[i] = byte(i % 256)
		}

		localBlocks = append(localBlocks, block)
		allocatedBytes += incrementBytes

		captureMetrics(routineID)

		time.Sleep(config.incrementTimeout)
	}

	fmt.Printf("Routine %d: Allocation duration: %v.\n", routineID, time.Since(allocationStart))

	var mmapReaders []*mmap.ReaderAt
	if fileCacheTargetBytes > 0 {
		for _, f := range tempFiles {
			r, err := mmap.Open(f.Name())
			if err != nil {
				fmt.Printf("Routine %d: Failed to mmap file %s: %v\n", routineID, f.Name(), err)
				continue
			}
			mmapReaders = append(mmapReaders, r)
		}
		defer func() {
			for _, r := range mmapReaders {
				r.Close()
			}
		}()
	}
	// After allocation (peak), get 'free' metrics
	m, s, err := getSystemMemory()
	if err != nil {
		fmt.Printf("Failed to get system memory: %v\n", err)
	}

	metrics.PeakSystemTotalMemory = m.total
	metrics.PeakSystemUsedMemory = m.used
	metrics.PeakSystemFreeMemory = m.free
	metrics.PeakSystemTotalSwap = s.total
	metrics.PeakSystemUsedSwap = s.used
	metrics.PeakSystemFreeSwap = s.free

	// Hold the memory
	holdStart := time.Now()
	sleepDuration := config.holdDuration
	fmt.Printf("Routine %d: Holding memory for %v using %s strategy.\n", routineID, sleepDuration, config.holdStrategy)

	holdUntil := time.Now().Add(sleepDuration)
	if config.holdStrategy == "sequential" {
		var seqBlockIndex, seqByteIndex int
		var seqFileIndex int
		var seqFileOffset int64
		const pageSize = 4096

		for time.Now().Before(holdUntil) {
			// Access anonymous memory sequentially
			if len(localBlocks) > 0 {
				block := localBlocks[seqBlockIndex]
				if len(block) > 0 {
					block[seqByteIndex]++ // Access one byte
				}
				seqByteIndex += pageSize
				if seqByteIndex >= len(block) {
					seqByteIndex = 0
					seqBlockIndex++
					if seqBlockIndex >= len(localBlocks) {
						seqBlockIndex = 0 // loop back
					}
				}
			}

			// Access file cache sequentially
			if len(mmapReaders) > 0 {
				r := mmapReaders[seqFileIndex]
				if r.Len() > 0 {
					_ = r.At(int(seqFileOffset)) // Access one byte.
					seqFileOffset += pageSize
					if seqFileOffset >= int64(r.Len()) {
						seqFileOffset = 0
						seqFileIndex++
						if seqFileIndex >= len(mmapReaders) {
							seqFileIndex = 0 // loop back
						}
					}
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	} else if config.holdStrategy == "hot" {
		for time.Now().Before(holdUntil) {
			// Access anonymous memory
			if len(localBlocks) > 0 {
				// Just touch one byte per block to keep it active
				blockIndex := rand.Intn(len(localBlocks))
				byteIndex := rand.Intn(len(localBlocks[blockIndex]))
				localBlocks[blockIndex][byteIndex]++
			}

			// Access file cache
			if len(mmapReaders) > 0 {
				readerIndex := rand.Intn(len(mmapReaders))
				r := mmapReaders[readerIndex]
				if r.Len() > 0 {
					offset := rand.Intn(r.Len())
					_ = r.At(offset) // Access one byte.
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	} else if config.holdStrategy == "sleep" {
		time.Sleep(sleepDuration)
	}
	fmt.Printf("Routine %d: Held memory for %v. Starting to release memory.\n", routineID, time.Since(holdStart))

	// Step down: Release memory gradually
	releaseStart := time.Now()
	for i := len(localBlocks) - 1; i >= 0; i-- {
		localBlocks[i] = nil
		time.Sleep(config.incrementTimeout)
	}
	fmt.Printf("Routine %d: Release duration: %v.\n", routineID, time.Since(releaseStart))
}

func captureMetrics(routineID int) {
	currentMemoryUsage := getAllocatedMemory()
	currentSwapUsage := getPodSwapUsage()
	peakMemoryUsage := getPodPeakMemoryUsage()
	peakSwapUsage := getPodPeakSwapUsage()

	m, _, err := getSystemMemory()
	var currentPageCache uint64
	if err != nil {
		fmt.Printf("Routine %d: Failed to get system memory for metrics: %v\n", routineID, err)
	} else {
		currentPageCache = m.buffCache
	}

	cpuAvg10Some, ioAvg10Some, memAvg10Some, memAvg10Full, cpuTotalSome, ioTotalSome, memTotalSome, memTotalFull := getPSIStats()

	metricsMutex.Lock()
	metrics.GrowthMetrics = append(metrics.GrowthMetrics, TimePoint{
		Timestamp:       time.Now(),
		PodMemoryUsage:  currentMemoryUsage,
		PodSwapUsage:    currentSwapUsage,
		SystemPageCache: currentPageCache,
		CPUAvg10Some:    cpuAvg10Some,
		IoAvg10Some:     ioAvg10Some,
		MemAvg10Some:    memAvg10Some,
		MemAvg10Full:    memAvg10Full,
		CPUTotalSome:    cpuTotalSome,
		IoTotalSome:     ioTotalSome,
		MemTotalSome:    memTotalSome,
		MemTotalFull:    memTotalFull,
	})
	metricsMutex.Unlock()

	fmt.Printf("Routine %d: Memory usage: %d bytes, Swap usage: %d bytes, Peak memory usage: %d bytes, Peak swap usage: %d bytes.\n", routineID, currentMemoryUsage, currentSwapUsage, peakMemoryUsage, peakSwapUsage)
}

func reportMetrics(config StressConfig) {
	metrics.FinalPodSwapUsage = getPodSwapUsage()
	metrics.PeakPodMemoryUsage = getPodPeakMemoryUsage()
	metrics.PeakPodSwapUsage = getPodPeakSwapUsage()

	report := FinalReport{
		Config: ReportConfig{
			TargetBytesMiB:    config.targetBytesMiB,
			FileCacheBytesMiB: config.fileCacheBytesMiB,
			IncrementBytesMiB: config.incrementBytesMiB,
			IncrementTimeout:  config.incrementTimeout,
			HoldDuration:      config.holdDuration,
			RoutineCount:      config.routineCount,
			HoldStrategy:      config.holdStrategy,
		},
		Metrics: metrics,
	}
	report.SystemInfo.Swappiness = getSwappiness()
	report.SystemInfo.PodID = getPodID()
	report.SystemInfo.QoSClass = getQosClass()
	report.SystemInfo.CgroupPath = cgroupPath
	report.SystemInfo.Kernel = runtime.Version()
	report.SystemInfo.OS = runtime.GOOS
	report.SystemInfo.Architecture = runtime.GOARCH

	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Printf("Failed to marshal report: %v\n", err)
		return
	}
	fileName := fmt.Sprintf("%dMiB_report_%s.json", config.incrementBytesMiB, metrics.StartTime.Format("2006-01-02_15-04-05"))
	filePath := config.reportPath + fileName
	if err := ioutil.WriteFile(filePath, reportJSON, 0644); err != nil {
		fmt.Printf("Failed to write report to %s: %v\n", filePath, err)
		return
	}
	fmt.Printf("Metrics report saved to %s\n", filePath)
}

func randomDuration(max time.Duration) time.Duration {
	return time.Duration(rand.Int63n(int64(max)))
}

// getAllocatedMemory returns the total memory allocated from the pod cgroup.
// This is the memory that is charged to the pod and is subject to OOM kills.
//
// See https://docs.kernel.org/admin-guide/cgroup-v2.html#memory-interface-files for more details.
func getAllocatedMemory() uint64 {
	b, err := ioutil.ReadFile(cgroupPath + "/memory.current")
	if err != nil {
		fmt.Printf("Failed to read memory.current: %v\n", err)
		return 0
	}
	bs := strings.TrimSpace(string(b))
	mem, err := strconv.ParseUint(bs, 10, 64)
	if err != nil {
		fmt.Printf("Failed to parse memory.current: %v\n", err)
		return 0
	}
	return mem
}

func getPodPeakMemoryUsage() uint64 {
	b, err := ioutil.ReadFile(cgroupPath + "/memory.peak")
	if err != nil {
		fmt.Printf("Failed to read memory.peak: %v\n", err)
		return 0
	}
	bs := strings.TrimSpace(string(b))
	mem, err := strconv.ParseUint(bs, 10, 64)
	if err != nil {
		fmt.Printf("Failed to parse memory.peak: %v\n", err)
		return 0
	}
	return mem
}

// getPodSwapUsage returns the total swap usage from the pod cgroup.
func getPodSwapUsage() uint64 {
	b, err := ioutil.ReadFile(cgroupPath + "/memory.swap.current")
	if err != nil {
		fmt.Printf("Failed to read memory.swap.current: %v\n", err)
		return 0
	}
	bs := strings.TrimSpace(string(b))
	mem, err := strconv.ParseUint(bs, 10, 64)
	if err != nil {
		fmt.Printf("Failed to parse memory.swap.current: %v\n", err)
		return 0
	}
	return mem
}

func getPodPeakSwapUsage() uint64 {
	b, err := ioutil.ReadFile(cgroupPath + "/memory.swap.peak")
	if err != nil {
		fmt.Printf("Failed to read memory.swap.peak: %v\n", err)
		return 0
	}
	bs := strings.TrimSpace(string(b))
	mem, err := strconv.ParseUint(bs, 10, 64)
	if err != nil {
		fmt.Printf("Failed to parse memory.swap.peak: %v\n", err)
		return 0
	}
	return mem
}

// parsePressureFile parses a PSI file and returns the avg10 values for "some" and "full".
func parsePressureFile(path string) (someAvg10 float64, fullAvg10 float64, someTotal uint64, fullTotal uint64) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		// File might not exist, which is not an error.
		return 0, 0, 0, 0
	}

	scanner := bufio.NewScanner(bytes.NewReader(b))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		var avg10 float64
		var total uint64
		for _, part := range parts[1:] {
			if strings.HasPrefix(part, "avg10=") {
				valStr := strings.TrimPrefix(part, "avg10=")
				avg10, _ = strconv.ParseFloat(valStr, 64) // Ignore parse error, will result in 0
			}
			if strings.HasPrefix(part, "total=") {
				valStr := strings.TrimPrefix(part, "total=")
				total, _ = strconv.ParseUint(valStr, 10, 64)
			}
		}

		switch parts[0] {
		case "some":
			someAvg10 = avg10
			someTotal = total
		case "full":
			fullAvg10 = avg10
			fullTotal = total
		}
	}
	return someAvg10, fullAvg10, someTotal, fullTotal
}

// getPSIStats reads PSI metrics from the cgroup.
func getPSIStats() (cpuSomeAvg10, ioSomeAvg10, memSomeAvg10, memFullAvg10 float64, cpuSomeTotal, ioSomeTotal, memSomeTotal, memFullTotal uint64) {
	cpuSomeAvg10, _, cpuSomeTotal, _ = parsePressureFile(cgroupPath + "/cpu.pressure")
	ioSomeAvg10, _, ioSomeTotal, _ = parsePressureFile(cgroupPath + "/io.pressure")
	memSomeAvg10, memFullAvg10, memSomeTotal, memFullTotal = parsePressureFile(cgroupPath + "/memory.pressure")
	return
}

func getSystemMemory() (memBytes *freeBytes, swapBytes *freeBytes, err error) {
	memBytes = new(freeBytes)
	swapBytes = new(freeBytes)
	cmd := exec.Command("free", "-b")
	op, err := cmd.Output()
	if err != nil {
		return memBytes, swapBytes, fmt.Errorf("Failed to run free: %w", err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(op))
	scanner.Scan() // skip header
	scanner.Scan() // memory
	line := scanner.Text()
	fields := strings.Fields(line)
	if len(fields) < 7 {
		return memBytes, swapBytes, fmt.Errorf("unexpected free output: %v", line)
	}
	mem, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return memBytes, swapBytes, fmt.Errorf("failed to parse total memory from %q: %v", line, err)
	}
	memBytes.total = mem
	mem, err = strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		return memBytes, swapBytes, fmt.Errorf("failed to parse used memory from %q: %v", line, err)
	}
	memBytes.used = mem
	mem, err = strconv.ParseUint(fields[3], 10, 64)
	if err != nil {
		return memBytes, swapBytes, fmt.Errorf("failed to parse free memory from %q: %v", line, err)
	}
	memBytes.free = mem
	mem, err = strconv.ParseUint(fields[5], 10, 64)
	if err != nil {
		return memBytes, swapBytes, fmt.Errorf("failed to parse buff/cache memory from %q: %v", line, err)
	}
	memBytes.buffCache = mem
	scanner.Scan() // swap
	line = scanner.Text()
	fields = strings.Fields(line)
	if len(fields) < 4 {
		return memBytes, swapBytes, fmt.Errorf("unexpected free output: %v", line)
	}
	swap, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return memBytes, swapBytes, fmt.Errorf("failed to parse total swap from %q: %v", line, err)
	}
	swapBytes.total = swap
	swap, err = strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		return memBytes, swapBytes, fmt.Errorf("failed to parse used swap from %q: %v", line, err)
	}
	swapBytes.used = swap
	swap, err = strconv.ParseUint(fields[3], 10, 64)
	if err != nil {
		return memBytes, swapBytes, fmt.Errorf("failed to parse free swap from %q: %v", line, err)
	}
	swapBytes.free = swap
	return memBytes, swapBytes, nil
}

func getSwappiness() int {
	b, err := ioutil.ReadFile("/proc/sys/vm/swappiness")
	if err != nil {
		fmt.Printf("Failed to read /proc/sys/vm/swappiness: %v\n", err)
		return -1
	}
	s, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		fmt.Printf("Failed to parse swappiness value: %v\n", err)
		return -1
	}
	return s
}

func getDownwardPath(s string) string {
	return "/etc/podinfo/" + s
}

// getPodID returns the pod UID of the current pod.
func getPodID() string {
	b, err := ioutil.ReadFile(getDownwardPath("pod_id"))
	if err != nil {
		fmt.Printf("Failed to read pod ID: %v\n", err)
		return "unknown"
	}
	return strings.TrimSpace(string(b))
}

// getQosClass returns the QoS class of the pod.
//
//	Note: This assumes a single container running in the pod (which is the case for this test)
func getQosClass() string {
	var memRequest, memLimit, cpuRequest, cpuLimit string
	b, err := ioutil.ReadFile(getDownwardPath("memory_request"))
	if err != nil {
		fmt.Printf("Failed to read memory request: %v\n", err)
		memRequest = "max"
	} else {
		memRequest = strings.TrimSpace(string(b))
		fmt.Printf("Memory request: %v\n", memRequest)
	}
	b, err = ioutil.ReadFile(getDownwardPath("memory_limit"))
	if err != nil {
		fmt.Printf("Failed to read memory limit: %v\n", err)
		memLimit = "max"
	} else {
		memLimit = strings.TrimSpace(string(b))
		fmt.Printf("Memory limit: %v\n", memLimit)
	}
	b, err = ioutil.ReadFile(getDownwardPath("cpu_request"))
	if err != nil {
		fmt.Printf("Failed to read cpu request: %v\n", err)
		cpuRequest = "max"
	} else {
		cpuRequest = strings.TrimSpace(string(b))
		fmt.Printf("CPU request: %v\n", cpuRequest)
	}
	b, err = ioutil.ReadFile(getDownwardPath("cpu_limit"))
	if err != nil {
		fmt.Printf("Failed to read cpu limit: %v\n", err)
		cpuLimit = "max"
	} else {
		cpuLimit = strings.TrimSpace(string(b))
		fmt.Printf("CPU limit: %v\n", cpuLimit)
	}
	if cpuRequest == "max" && cpuLimit == "max" && memRequest == "max" && memLimit == "max" {
		return "besteffort"
	}
	if memRequest == memLimit && cpuRequest == cpuLimit {
		return "guaranteed"
	}
	return "burstable"
}

func getPodCgroupPath() string {
	podID := getPodID()
	qosClass := getQosClass()
	return fmt.Sprintf("/sys/fs/cgroup/kubepods.slice/kubepods-%s.slice/kubepods-%s-pod%s.slice",
		qosClass, qosClass, strings.ReplaceAll(podID, "-", "_"))
}

func createAndPopulateFiles(routineID int, totalSize uint64, config StressConfig) ([]*os.File, error) {
	var files []*os.File
	// Create multiple files to better exercise the page cache.
	numFiles := 10
	if totalSize < 100*1024*1024 { // Less than 100MiB
		numFiles = int(totalSize / (10 * 1024 * 1024)) // 1 file per 10MiB
	}
	if numFiles == 0 {
		numFiles = 1
	}
	sizePerFile := totalSize / uint64(numFiles)

	for i := 0; i < numFiles; i++ {
		file, err := os.CreateTemp(config.fileCachePath, fmt.Sprintf("file-cache-stress-%d-%d-", routineID, i))
		if err != nil {
			return files, fmt.Errorf("failed to create temp file: %w", err)
		}
		files = append(files, file)

		// Write random data to the file.
		if _, err := file.Seek(int64(sizePerFile-1), 0); err != nil {
			return files, fmt.Errorf("failed to seek in temp file: %w", err)
		}
		if _, err := file.Write([]byte{0}); err != nil {
			return files, fmt.Errorf("failed to write to temp file to expand it: %w", err)
		}

		// Populate the file with random data to ensure it's read into page cache.
		if _, err := file.Seek(0, 0); err != nil {
			return files, fmt.Errorf("failed to seek to start of temp file: %w", err)
		}

		buffer := make([]byte, 1024*1024) // 1MB buffer
		var writtenBytes uint64
		for writtenBytes < sizePerFile {
			rand.Read(buffer)
			bytesToWrite := len(buffer)
			if writtenBytes+uint64(bytesToWrite) > sizePerFile {
				bytesToWrite = int(sizePerFile - writtenBytes)
			}
			n, err := file.Write(buffer[:bytesToWrite])
			if err != nil {
				return files, fmt.Errorf("failed to write data to temp file: %w", err)
			}
			writtenBytes += uint64(n)
			// captureMetrics(routineID)
			// time.Sleep(config.incrementTimeout)
		}
		fmt.Printf("Routine %d: Created and populated %s (%d bytes)\n", routineID, file.Name(), sizePerFile)
	}

	return files, nil
}
