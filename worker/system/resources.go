package system

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// ResourceStatus contains current system resource usage
type ResourceStatus struct {
	MemoryUsagePercent float64
	MemoryUsedMB       uint64
	MemoryTotalMB      uint64
	NumCPU             int
	NumGoroutine       int
}

// ResourceLimits defines thresholds for resource-aware job processing
type ResourceLimits struct {
	MaxMemoryPercent float64 // Maximum memory usage percentage (e.g., 85.0)
}

// DefaultResourceLimits returns sensible defaults
func DefaultResourceLimits() ResourceLimits {
	return ResourceLimits{
		MaxMemoryPercent: 85.0,
	}
}

// GetResourceStatus returns current system resource usage
func GetResourceStatus() ResourceStatus {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	status := ResourceStatus{
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
	}

	// Get system memory info (platform-specific)
	totalMem, availMem := getSystemMemory()
	if totalMem > 0 {
		usedMem := totalMem - availMem
		status.MemoryTotalMB = totalMem / (1024 * 1024)
		status.MemoryUsedMB = usedMem / (1024 * 1024)
		status.MemoryUsagePercent = float64(usedMem) / float64(totalMem) * 100
	} else {
		// Fallback to Go runtime stats (heap only, less accurate)
		status.MemoryUsedMB = m.Alloc / (1024 * 1024)
		status.MemoryTotalMB = m.Sys / (1024 * 1024)
		if m.Sys > 0 {
			status.MemoryUsagePercent = float64(m.Alloc) / float64(m.Sys) * 100
		}
	}

	return status
}

// CheckResourcesAvailable returns true if system has enough resources to accept a new job
func CheckResourcesAvailable(limits ResourceLimits) (bool, string) {
	status := GetResourceStatus()

	if status.MemoryUsagePercent > limits.MaxMemoryPercent {
		return false, "memory usage too high: " + strconv.FormatFloat(status.MemoryUsagePercent, 'f', 1, 64) + "%"
	}

	return true, ""
}

// getSystemMemory returns total and available system memory in bytes
// Returns 0, 0 if unable to determine
func getSystemMemory() (total uint64, available uint64) {
	switch runtime.GOOS {
	case "linux":
		return getLinuxMemory()
	case "darwin":
		return getDarwinMemory()
	default:
		return 0, 0
	}
}

// getLinuxMemory reads memory info from /proc/meminfo
func getLinuxMemory() (total uint64, available uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var memTotal, memAvailable, memFree, buffers, cached uint64

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		val, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		val *= 1024 // Convert from KB to bytes

		switch fields[0] {
		case "MemTotal:":
			memTotal = val
		case "MemAvailable:":
			memAvailable = val
		case "MemFree:":
			memFree = val
		case "Buffers:":
			buffers = val
		case "Cached:":
			cached = val
		}
	}

	total = memTotal
	if memAvailable > 0 {
		available = memAvailable
	} else {
		// Fallback for older kernels without MemAvailable
		available = memFree + buffers + cached
	}

	return total, available
}

// getDarwinMemory uses vm_stat for macOS memory info
func getDarwinMemory() (total uint64, available uint64) {
	// On macOS, we can use sysctl for total memory
	// For simplicity, use a reasonable estimate based on runtime
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// This is a rough approximation - actual system memory detection
	// on macOS would require cgo or syscall usage
	// For container environments (Docker), /proc/meminfo should work
	return 0, 0
}
