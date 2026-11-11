package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	goruntime "runtime"
	"runtime/debug"
	"unsafe"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/infra"
)

// pprofEnv names the loopback address of the diagnostic profiling server, such as 127.0.0.1:6060.
const pprofEnv = "NAHIDA_PPROF"

// startProfiler serves net/http/pprof and process memory figures when NAHIDA_PPROF is set. It only
// listens on a loopback address, because heap profiles expose file paths and other user data.
func startProfiler(log *infra.Log) {
	addr := os.Getenv(pprofEnv)
	if addr == "" {
		return
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		log.Warn(map[string]any{"address": addr, "reason": "not a loopback host:port"}, "Profiler")
		return
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		_ = infra.ReportError(log, err, "Profiler", infra.Diagnostic{Operation: "listen", Fields: map[string]any{
			"address": addr,
		}})
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.HandleFunc("/debug/memory", serveMemory)
	mux.HandleFunc("/debug/memory/free", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		debug.FreeOSMemory()
		serveMemory(w, r)
	})
	log.Info(map[string]any{"address": listener.Addr().String()}, "Profiler")
	go func() {
		err := http.Serve(listener, mux) //nolint:gosec // loopback-only diagnostic server enabled by NAHIDA_PPROF
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = infra.ReportError(log, err, "Profiler", infra.Diagnostic{Operation: "serve"})
		}
	}()
}

type memoryReport struct {
	// Process figures come from Windows and include native allocations (D3D, WIC, DirectWrite, SQLite)
	// that the Go heap figures below do not.
	ProcessPrivateMiB    float64 `json:"processPrivateMiB"`
	ProcessWorkingSetMiB float64 `json:"processWorkingSetMiB"`
	GoSysMiB             float64 `json:"goSysMiB"`
	GoHeapAllocMiB       float64 `json:"goHeapAllocMiB"`
	GoHeapInuseMiB       float64 `json:"goHeapInuseMiB"`
	GoHeapIdleMiB        float64 `json:"goHeapIdleMiB"`
	GoHeapReleasedMiB    float64 `json:"goHeapReleasedMiB"`
	GoStackInuseMiB      float64 `json:"goStackInuseMiB"`
	GoNextGCMiB          float64 `json:"goNextGCMiB"`
	GoHeapObjects        uint64  `json:"goHeapObjects"`
	Goroutines           int     `json:"goroutines"`
	NumGC                uint32  `json:"numGC"`
	ProcessError         string  `json:"processError,omitempty"`
}

func serveMemory(w http.ResponseWriter, _ *http.Request) {
	var stats goruntime.MemStats
	goruntime.ReadMemStats(&stats)
	report := memoryReport{
		GoSysMiB:          mib(stats.Sys),
		GoHeapAllocMiB:    mib(stats.HeapAlloc),
		GoHeapInuseMiB:    mib(stats.HeapInuse),
		GoHeapIdleMiB:     mib(stats.HeapIdle),
		GoHeapReleasedMiB: mib(stats.HeapReleased),
		GoStackInuseMiB:   mib(stats.StackInuse),
		GoNextGCMiB:       mib(stats.NextGC),
		GoHeapObjects:     stats.HeapObjects,
		Goroutines:        goruntime.NumGoroutine(),
		NumGC:             stats.NumGC,
	}
	if counters, err := processMemory(); err != nil {
		report.ProcessError = err.Error()
	} else {
		report.ProcessPrivateMiB = mib(uint64(counters.PrivateUsage))
		report.ProcessWorkingSetMiB = mib(uint64(counters.WorkingSetSize))
	}
	w.Header().Set("Content-Type", "application/json")
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(report)
}

func mib(n uint64) float64 { return float64(n) / (1 << 20) }

// processMemoryCounters is PROCESS_MEMORY_COUNTERS_EX.
type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
}

var procGetProcessMemoryInfo = windows.NewLazySystemDLL("kernel32.dll").NewProc("K32GetProcessMemoryInfo")

func processMemory() (processMemoryCounters, error) {
	var counters processMemoryCounters
	counters.CB = uint32(unsafe.Sizeof(counters))
	ok, _, err := procGetProcessMemoryInfo.Call(
		uintptr(windows.CurrentProcess()),
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.CB),
	)
	if ok == 0 {
		return counters, fmt.Errorf("K32GetProcessMemoryInfo: %w", err)
	}
	return counters, nil
}
