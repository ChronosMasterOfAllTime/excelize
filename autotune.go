// Copyright 2016 - 2026 The excelize Authors. All rights reserved. Use of
// this source code is governed by a BSD-style license that can be found in
// the LICENSE file.

package excelize

import "fmt"

// AutoTuneProfile is implemented by each streaming-profile type. Its Tune
// method receives the machine's currently available memory and the free space
// in the OS temp directory (both in bytes) and returns recommended streaming
// I/O settings. Fields already set to non-zero values in Options are never
// overridden, so profiles can be mixed with explicit overrides.
//
// availDisk is -1 when the query fails; implementations must treat a negative
// value as "disk space unknown" and skip any disk-based logic.
//
// The four built-in profiles are AutoTuneNone, AutoTuneMemoryOptimized,
// AutoTuneDiskOptimized, and AutoTuneBalanced. The zero value of
// Options.AutoTune (nil) behaves identically to AutoTuneNone.
type AutoTuneProfile interface {
	Tune(availMem, availDisk int64) autoTuneSettings
}

// autoTuneSettings holds the streaming I/O parameters resolved by a profile.
// A zero value for any field means "leave the option unchanged".
type autoTuneSettings struct {
	chunkSize   int64
	bufSize     int64
	compression Compression
}

// String returns a human-readable description of the resolved settings.
// Intended for debugging and logging only; the format is not stable.
func (s autoTuneSettings) String() string {
	chunk := "default"
	if s.chunkSize < 0 {
		chunk = "never-spill"
	} else if s.chunkSize > 0 {
		chunk = fmt.Sprintf("%d KiB", s.chunkSize>>10)
	}
	buf := "default"
	if s.bufSize > 0 {
		buf = fmt.Sprintf("%d KiB", s.bufSize>>10)
	}
	return fmt.Sprintf("autoTuneSettings{chunkSize:%s bufSize:%s compression:%v}", chunk, buf, s.compression)
}

// --- concrete profile types -------------------------------------------------

type (
	autoTuneNoneProfile     struct{}
	autoTuneMemoryProfile   struct{}
	autoTuneDiskProfile     struct{}
	autoTuneBalancedProfile struct{}
)

func (autoTuneNoneProfile) Tune(_, _ int64) autoTuneSettings { return autoTuneSettings{} }

// AutoTuneMemoryOptimized minimises peak heap usage by spilling XML data to a
// temp file early and using standard deflate compression (which produces the
// smallest ZIP output and therefore the least data traversing the output
// pipeline).
//
// Streaming thresholds are derived from available memory and disk space:
//
//	ChunkSize   = clamp(availMem/32, 1 MiB, 4 MiB)
//	            = -1 (never spill) when availDisk < 1 GiB
//	BufSize     = 32 KiB
//	Compression = CompressionDefault
func (autoTuneMemoryProfile) Tune(availMem, availDisk int64) autoTuneSettings {
	chunk := clampUint64(availMem/32, autoTuneMinChunk, 4<<20)
	if availDisk >= 0 && availDisk < autoTuneDiskSpillMin {
		chunk = -1 // disk too full to spill safely; keep everything in memory
	}
	return autoTuneSettings{
		chunkSize: chunk,
		bufSize:   autoTuneMinBuf,
		// CompressionDefault == 0; leaving compression zero means "no change".
	}
}

// AutoTuneDiskOptimized minimises disk I/O by keeping XML data in memory as
// long as possible and writing large batches when the threshold is eventually
// crossed. Disabling ZIP compression further reduces write amplification.
//
// Streaming thresholds are derived from available memory and disk space:
//
//	ChunkSize   = -1 (never spill) when availMem >= 2 GiB or availDisk < 1 GiB,
//	              otherwise clamp(availMem/2, 64 MiB, 512 MiB)
//	BufSize     = clamp(availMem/1000, 512 KiB, 4 MiB)
//	Compression = CompressionNone
func (autoTuneDiskProfile) Tune(availMem, availDisk int64) autoTuneSettings {
	chunk := clampUint64(availMem/2, 64<<20, autoTuneMaxChunk)
	const twoGiB = 2 << 30
	if availMem >= twoGiB || (availDisk >= 0 && availDisk < autoTuneDiskSpillMin) {
		chunk = -1 // never spill: RAM is plentiful or disk is nearly full
	}
	return autoTuneSettings{
		chunkSize:   chunk,
		bufSize:     clampUint64(availMem/1000, 512<<10, autoTuneMaxBuf),
		compression: CompressionNone,
	}
}

// AutoTuneBalanced splits the workload evenly: moderate chunk size, 256 KiB
// write buffer, and best-speed compression.
//
// Streaming thresholds are derived from available memory and disk space:
//
//	ChunkSize   = clamp(availMem/8, 16 MiB, 64 MiB)
//	            = -1 (never spill) when availDisk < 1 GiB
//	BufSize     = 256 KiB
//	Compression = CompressionBestSpeed
func (autoTuneBalancedProfile) Tune(availMem, availDisk int64) autoTuneSettings {
	chunk := clampUint64(availMem/8, 16<<20, 64<<20)
	if availDisk >= 0 && availDisk < autoTuneDiskSpillMin {
		chunk = -1 // disk too full to spill safely; keep everything in memory
	}
	return autoTuneSettings{
		chunkSize:   chunk,
		bufSize:     256 << 10,
		compression: CompressionBestSpeed,
	}
}

// --- exported profile singletons --------------------------------------------

var (
	// AutoTuneNone disables auto-tuning (the default). StreamingChunkSize,
	// StreamingBufSize, and Compression are used as-is.
	AutoTuneNone AutoTuneProfile = autoTuneNoneProfile{}

	// AutoTuneMemoryOptimized minimises peak heap usage. See
	// autoTuneMemoryProfile.tune for details.
	AutoTuneMemoryOptimized AutoTuneProfile = autoTuneMemoryProfile{}

	// AutoTuneDiskOptimized minimises disk I/O. See autoTuneDiskProfile.tune
	// for details.
	AutoTuneDiskOptimized AutoTuneProfile = autoTuneDiskProfile{}

	// AutoTuneBalanced splits the workload evenly. See
	// autoTuneBalancedProfile.tune for details.
	AutoTuneBalanced AutoTuneProfile = autoTuneBalancedProfile{}
)

const (
	// autoTuneFallbackMem is the assumed available memory when the OS query
	// fails. 4 GiB is a conservative but reasonable modern baseline.
	autoTuneFallbackMem int64 = 4 << 30 // 4 GiB

	autoTuneMinChunk int64 = 1 << 20   // 1 MiB
	autoTuneMaxChunk int64 = 512 << 20 // 512 MiB

	autoTuneMinBuf int64 = 32 << 10 // 32 KiB
	autoTuneMaxBuf int64 = 4 << 20  // 4 MiB

	// autoTuneDiskSpillMin is the minimum free space required in the OS temp
	// directory for any profile to spill XML data to disk. When free space
	// drops below this threshold, the chunk size is forced to -1 (never
	// spill) regardless of memory pressure.
	autoTuneDiskSpillMin int64 = 1 << 30 // 1 GiB
)

// clampUInt64 returns v clamped to [lo, hi].
func clampUint64(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// applyAutoTune fills in zero-value streaming fields of opts by delegating to
// the AutoTune profile's tune method. Fields already set by the caller are
// never modified.
//
// applyAutoTune is idempotent: calling it a second time is safe because the
// fields are no longer zero after the first call.
func applyAutoTune(opts *Options) {
	if opts == nil || opts.AutoTune == nil {
		return
	}

	availMem := availableMemoryBytes()
	// Guard against overflow when availMem is very large (>= max int on 32-bit).
	if availMem <= 0 {
		availMem = autoTuneFallbackMem
	}
	availDisk := availableDiskBytes()

	s := opts.AutoTune.Tune(availMem, availDisk)
	if opts.StreamingChunkSize == 0 && s.chunkSize != 0 {
		opts.StreamingChunkSize = int(s.chunkSize)
	}
	if opts.StreamingBufSize == 0 && s.bufSize != 0 {
		opts.StreamingBufSize = int(s.bufSize)
	}
	if opts.Compression == CompressionDefault && s.compression != CompressionDefault {
		opts.Compression = s.compression
	}
}
