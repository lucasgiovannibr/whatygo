// Package runtimetune sets the Go runtime up for the container it runs in.
package runtimetune

import (
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

// A Go program does not know that its container has a memory limit: with the default GOGC=100 the
// heap may grow to twice what is alive before a collection, and garbage that has not been collected
// yet counts toward the limit, so a process whose live data fits can still be killed (OOM) in a
// burst of media. A soft memory limit makes the collector work harder as the process nears it.
//
// Apply sets it to a share of the container's limit (MEMORY_LIMIT_RATIO, default 0.8) when the
// container has one and GOMEMLIMIT is not set; GOMEMLIMIT, when set, is honoured by the runtime and
// left alone. MEMORY_LIMIT_RATIO=0 turns this off.

const defaultRatio = 0.8

// cgroup files holding the memory limit: v2, then v1.
var cgroupFiles = []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"}

// noLimit is what cgroup v1 reports when there is none (a bit under 2^63); anything this big is
// not a limit.
const noLimit = int64(1) << 60

// containerLimit returns the memory limit in bytes of the container, or 0 when there is none.
func containerLimit(read func(string) ([]byte, error)) int64 {
	for _, f := range cgroupFiles {
		b, err := read(f)
		if err != nil {
			continue
		}
		v := strings.TrimSpace(string(b))
		if v == "" || v == "max" {
			return 0
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 || n >= noLimit {
			return 0
		}
		return n
	}
	return 0
}

// limitFor is the soft limit to set for a container limit and a ratio, 0 for none.
func limitFor(containerBytes int64, ratio float64) int64 {
	if containerBytes <= 0 || ratio <= 0 || ratio >= 1 {
		return 0
	}
	return int64(float64(containerBytes) * ratio)
}

func ratioFromEnv(value string) float64 {
	if v, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && v >= 0 && v < 1 {
		return v
	}
	return defaultRatio
}

// Apply sets the soft memory limit from the container's. log receives one line saying what was done.
func Apply(log func(format string, args ...interface{})) {
	apply(os.Getenv, os.ReadFile, debug.SetMemoryLimit, log)
}

func apply(getenv func(string) string, read func(string) ([]byte, error), set func(int64) int64, log func(string, ...interface{})) {
	if getenv("GOMEMLIMIT") != "" {
		return // the runtime reads it itself
	}
	limit := limitFor(containerLimit(read), ratioFromEnv(getenv("MEMORY_LIMIT_RATIO")))
	if limit == 0 {
		return
	}
	set(limit)
	log("[RUNTIME] Container memory limit found: Go soft memory limit set to %d MB (MEMORY_LIMIT_RATIO, or GOMEMLIMIT, changes it)", limit>>20)
}
