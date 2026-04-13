# Future: Dynamic Disk I/O Pressure in AutoTune

## Problem

`applyAutoTune` is called once at `NewStreamWriter` time and makes a one-shot
decision. `disk.IOCounters()` only exposes **cumulative** per-device counters
(`ReadBytes`, `WriteBytes`, `IopsInProgress`, `IoTime`, `WeightedIO`). A
single read tells you nothing about current load — pressure must be derived
by computing the delta between two samples separated by a wall-clock interval.

The free-space guard added in this PR (`autoTuneDiskSpillMin`) is the correct
primitive for a one-shot decision. I/O pressure is a separate, richer signal.

---

## Proposed Design

### 1. Background pressure sampler

Add a package-level singleton sampler that starts a goroutine the first time
any `AutoTuneProfile` is used:

```go
// ioPressureSampler polls disk.IOCounters every pollInterval and maintains
// a rolling utilisation estimate for the device hosting os.TempDir().
type ioPressureSampler struct {
    mu        sync.RWMutex
    utilPct   float64   // 0–100, last computed window
    lastStat  disk.IOCountersStat
    lastTime  time.Time
}

var globalPressure = &ioPressureSampler{}

func (s *ioPressureSampler) start(pollInterval time.Duration) { … }
func (s *ioPressureSampler) Utilisation() float64 { … }
```

Linux exposes `IoTime` (ms busy) per device; `%util = ΔIoTime / (Δwall_ms)`.
macOS does not populate `IoTime` reliably — fall back to a weighted-IOPS proxy:
`utilProxy = clamp(WeightedIO_delta / maxExpectedIOPS, 0, 1)`.

### 2. New interface method (breaking — requires major version bump or opt-in)

Option A — extend the existing interface (breaking):
```go
type AutoTuneProfile interface {
    Tune(availMem, availDisk int64, diskUtilPct float64) autoTuneSettings
}
```

Option B — add a separate optional interface (non-breaking):
```go
type AutoTunePressureAware interface {
    AutoTuneProfile
    TuneWithPressure(availMem, availDisk int64, diskUtilPct float64) autoTuneSettings
}
```
`applyAutoTune` checks for `AutoTunePressureAware` first via type assertion;
falls back to `Tune(availMem, availDisk)` if not implemented.

**Recommendation**: Option B — preserves backward compatibility with any
external implementations of `AutoTuneProfile`.

### 3. Profile behaviour under pressure

| Profile | diskUtilPct threshold | Action |
|---|---|---|
| `AutoTuneMemoryOptimized` | ≥ 80 % | halve `chunkSize` (flush more often, smaller writes) |
| `AutoTuneDiskOptimized` | ≥ 80 % | force `chunkSize = -1` (never spill; disk is the bottleneck) |
| `AutoTuneBalanced` | ≥ 60 % | reduce `chunkSize` to lower bound (16 MiB); increase `bufSize` to 1 MiB to batch small writes |

### 4. Lifecycle & teardown

The sampler goroutine must be stopped when the last open `StreamWriter` is
closed. Use a reference-counted `sync.WaitGroup` + `context.CancelFunc`
attached to the `File` handle, or a package-level idle timer that stops the
sampler after N seconds of no active writers.

### 5. Testing

- Unit tests: inject a fake `IOCountersStat` provider via interface; assert
  that `TuneWithPressure` returns expected settings at 0 %, 79 %, 80 %, 100 %.
- Integration test: run a stream write while a background goroutine generates
  synthetic I/O load; verify the sampler's `Utilisation()` responds.
- Platform CI: Linux + macOS required (Windows `IoTime` is always 0).

---

## Dependencies

- `github.com/shirou/gopsutil/v4/disk` — already a direct dependency.
- No new imports required.

## Blocked on

- Decision on interface versioning strategy (Option A vs B).
- Confirmation that the sampler goroutine lifecycle integrates cleanly with
  `File.Close()` without leaking goroutines in short-lived test binaries.
