// Copyright 2016 - 2026 The excelize Authors. All rights reserved. Use of
// this source code is governed by a BSD-style license that can be found in
// the LICENSE file.

package excelize

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAutoTuneNone verifies that AutoTuneNone leaves options unchanged.
func TestAutoTuneNone(t *testing.T) {
	opts := &Options{AutoTune: AutoTuneNone}
	applyAutoTune(opts)
	assert.Equal(t, 0, opts.StreamingChunkSize)
	assert.Equal(t, 0, opts.StreamingBufSize)
	assert.Equal(t, CompressionDefault, opts.Compression)
}

// TestAutoTuneNilOptions ensures applyAutoTune does not panic on nil.
func TestAutoTuneNilOptions(t *testing.T) {
	assert.NotPanics(t, func() { applyAutoTune(nil) })
}

// TestAutoTuneMemoryOptimized checks that MemoryOptimized sets a small chunk
// and buffer while leaving Compression at the default (0).
func TestAutoTuneMemoryOptimized(t *testing.T) {
	opts := &Options{AutoTune: AutoTuneMemoryOptimized}
	applyAutoTune(opts)

	// chunk is either -1 (disk safety guard fired) or in [1 MiB, 4 MiB]
	assert.True(t, opts.StreamingChunkSize == -1 ||
		(opts.StreamingChunkSize >= int(autoTuneMinChunk) && opts.StreamingChunkSize <= 4<<20),
		"chunk must be -1 or in [1 MiB, 4 MiB], got %d", opts.StreamingChunkSize)

	assert.Equal(t, int(autoTuneMinBuf), opts.StreamingBufSize, "buf size must be 32 KiB")
	assert.Equal(t, CompressionDefault, opts.Compression, "compression must remain Default")
}

// TestAutoTuneMemoryOptimizedRespectExplicitFields verifies that explicit
// non-zero option values are not overridden.
func TestAutoTuneMemoryOptimizedRespectExplicitFields(t *testing.T) {
	const customChunk = 2 << 20 // 2 MiB
	const customBuf = 64 << 10  // 64 KiB
	opts := &Options{
		AutoTune:           AutoTuneMemoryOptimized,
		StreamingChunkSize: customChunk,
		StreamingBufSize:   customBuf,
	}
	applyAutoTune(opts)
	assert.Equal(t, customChunk, opts.StreamingChunkSize, "explicit chunk must not be overridden")
	assert.Equal(t, customBuf, opts.StreamingBufSize, "explicit buf must not be overridden")
}

// TestAutoTuneDiskOptimized checks that DiskOptimized sets a large chunk (or
// -1) and a large buffer, and selects CompressionNone.
func TestAutoTuneDiskOptimized(t *testing.T) {
	opts := &Options{AutoTune: AutoTuneDiskOptimized}
	applyAutoTune(opts)

	// chunk is either -1 (never spill) or a large positive value
	assert.True(t, opts.StreamingChunkSize == -1 || opts.StreamingChunkSize >= 64<<20,
		"disk-optimised chunk must be -1 or ≥ 64 MiB, got %d", opts.StreamingChunkSize)

	assert.GreaterOrEqual(t, opts.StreamingBufSize, 512<<10, "buf size must be ≥ 512 KiB")
	assert.LessOrEqual(t, opts.StreamingBufSize, int(autoTuneMaxBuf), "buf size must be ≤ 4 MiB")

	assert.Equal(t, CompressionNone, opts.Compression, "disk-optimised must use CompressionNone")
}

// TestAutoTuneDiskOptimizedRespectExplicitCompression verifies that an
// explicit Compression choice is not overridden by the profile.
func TestAutoTuneDiskOptimizedRespectExplicitCompression(t *testing.T) {
	opts := &Options{
		AutoTune:    AutoTuneDiskOptimized,
		Compression: CompressionBestSpeed,
	}
	applyAutoTune(opts)
	assert.Equal(t, CompressionBestSpeed, opts.Compression, "explicit compression must not be overridden")
}

// TestAutoTuneBalanced checks that Balanced sets moderate values and
// selects CompressionBestSpeed.
func TestAutoTuneBalanced(t *testing.T) {
	opts := &Options{AutoTune: AutoTuneBalanced}
	applyAutoTune(opts)

	// chunk is either -1 (disk safety guard fired) or in [16 MiB, 64 MiB]
	assert.True(t, opts.StreamingChunkSize == -1 ||
		(opts.StreamingChunkSize >= 16<<20 && opts.StreamingChunkSize <= 64<<20),
		"balanced chunk must be -1 or in [16 MiB, 64 MiB], got %d", opts.StreamingChunkSize)

	assert.Equal(t, 256<<10, opts.StreamingBufSize, "balanced buf size must be 256 KiB")
	assert.Equal(t, CompressionBestSpeed, opts.Compression, "balanced must use CompressionBestSpeed")
}

// TestAutoTuneIdempotent verifies that calling applyAutoTune twice does not
// change the options a second time.
func TestAutoTuneIdempotent(t *testing.T) {
	opts := &Options{AutoTune: AutoTuneBalanced}
	applyAutoTune(opts)
	chunk1, buf1, comp1 := opts.StreamingChunkSize, opts.StreamingBufSize, opts.Compression
	applyAutoTune(opts)
	assert.Equal(t, chunk1, opts.StreamingChunkSize)
	assert.Equal(t, buf1, opts.StreamingBufSize)
	assert.Equal(t, comp1, opts.Compression)
}

// TestAutoTuneEndToEndMemoryOptimized performs a full round-trip write with
// AutoTuneMemoryOptimized and verifies the output is a valid XLSX.
func TestAutoTuneEndToEndMemoryOptimized(t *testing.T) {
	autoTuneEndToEnd(t, AutoTuneMemoryOptimized)
}

// TestAutoTuneEndToEndDiskOptimized performs a full round-trip write with
// AutoTuneDiskOptimized and verifies the output is a valid XLSX.
func TestAutoTuneEndToEndDiskOptimized(t *testing.T) {
	autoTuneEndToEnd(t, AutoTuneDiskOptimized)
}

// TestAutoTuneEndToEndBalanced performs a full round-trip write with
// AutoTuneBalanced and verifies the output is a valid XLSX.
func TestAutoTuneEndToEndBalanced(t *testing.T) {
	autoTuneEndToEnd(t, AutoTuneBalanced)
}

// autoTuneEndToEnd is a helper that writes 200 rows with the given profile
// and round-trips through OpenReader.
func autoTuneEndToEnd(t *testing.T, profile AutoTuneProfile) {
	t.Helper()
	f := NewFile(Options{AutoTune: profile})
	sw, err := f.NewStreamWriter("Sheet1")
	require.NoError(t, err)

	const rows, cols = 200, 10
	for row := 1; row <= rows; row++ {
		data := make([]interface{}, cols)
		for col := range data {
			data[col] = row*cols + col
		}
		cell, err := CoordinatesToCellName(1, row)
		require.NoError(t, err)
		require.NoError(t, sw.SetRow(cell, data))
	}
	require.NoError(t, sw.Flush())

	var buf bytes.Buffer
	_, err = f.WriteTo(&buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// Verify the file is a valid XLSX and the first cell has the right value.
	f2, err := OpenReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	val, err := f2.GetCellValue("Sheet1", "A1")
	require.NoError(t, err)
	assert.Equal(t, "10", val) // row=1, col=0 → 1*10+0 = 10
	require.NoError(t, f2.Close())
}

// TestAutoTuneSettingsString verifies the String() method output for all
// meaningful field combinations. The format is for debugging only, but the
// cases below document the expected tokens so regressions are caught.
func TestAutoTuneSettingsString(t *testing.T) {
	cases := []struct {
		name     string
		s        autoTuneSettings
		wantSubs []string // substrings that must appear in the output
	}{
		{
			name:     "zero value",
			s:        autoTuneSettings{},
			wantSubs: []string{"chunkSize:default", "bufSize:default"},
		},
		{
			name:     "never-spill sentinel",
			s:        autoTuneSettings{chunkSize: -1, bufSize: 4 << 20},
			wantSubs: []string{"chunkSize:never-spill", "bufSize:4096 KiB"},
		},
		{
			name:     "positive chunk and buf",
			s:        autoTuneSettings{chunkSize: 16 << 20, bufSize: 256 << 10},
			wantSubs: []string{"chunkSize:16384 KiB", "bufSize:256 KiB"},
		},
		{
			name:     "compression none",
			s:        autoTuneSettings{compression: CompressionNone},
			wantSubs: []string{"compression:"},
		},
		{
			name:     "compression best speed",
			s:        autoTuneSettings{chunkSize: 1 << 20, bufSize: 32 << 10, compression: CompressionBestSpeed},
			wantSubs: []string{"chunkSize:1024 KiB", "bufSize:32 KiB"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.s.String()
			for _, sub := range tc.wantSubs {
				assert.Contains(t, got, sub, "String() output %q missing %q", got, sub)
			}
		})
	}
}

// TestAvailableMemoryBytes sanity-checks that availableMemoryBytes returns
// a plausible positive value.
func TestAvailableMemoryBytes(t *testing.T) {
	mem := availableMemoryBytes()
	assert.Greater(t, mem, int64(0), "available memory must be positive")
	// Any reasonable machine has at least 256 MiB
	assert.GreaterOrEqual(t, mem, int64(256<<20), "expected at least 256 MiB available")
}

// TestAvailableDiskBytes sanity-checks that availableDiskBytes returns either
// -1 (query failed) or a positive value.
func TestAvailableDiskBytes(t *testing.T) {
	d := availableDiskBytes()
	if d == -1 {
		t.Log("availableDiskBytes returned -1 (query failed on this platform)")
		return
	}
	assert.Greater(t, d, int64(0), "available disk must be positive")
}

// TestAutoTuneDiskSpillGuard verifies that profiles force chunkSize = -1
// when availDisk is below the safety threshold.
func TestAutoTuneDiskSpillGuard(t *testing.T) {
	tinyDisk := autoTuneDiskSpillMin - 1 // just below threshold
	enoughMem := int64(8 << 30)          // 8 GiB — plenty of RAM

	// MemoryOptimized: would normally spill, but must back off
	s := autoTuneMemoryProfile{}.Tune(enoughMem, tinyDisk)
	assert.Equal(t, int64(-1), s.chunkSize, "MemoryOptimized must never-spill when disk is tight")

	// Balanced: same expectation
	s = autoTuneBalancedProfile{}.Tune(enoughMem, tinyDisk)
	assert.Equal(t, int64(-1), s.chunkSize, "Balanced must never-spill when disk is tight")

	// DiskOptimized with RAM < 2 GiB: would normally spill, but must back off
	s = autoTuneDiskProfile{}.Tune(1<<30, tinyDisk)
	assert.Equal(t, int64(-1), s.chunkSize, "DiskOptimized must never-spill when disk is tight")
}

// TestAutoTuneDiskSpillGuardAboveThreshold verifies that profiles use
// normal positive chunk sizes when disk space is plentiful.
func TestAutoTuneDiskSpillGuardAboveThreshold(t *testing.T) {
	enoughDisk := autoTuneDiskSpillMin + int64(1<<30) // 1 GiB above threshold
	enoughMem := int64(8 << 30)

	s := autoTuneMemoryProfile{}.Tune(enoughMem, enoughDisk)
	assert.Greater(t, s.chunkSize, int64(0), "MemoryOptimized must use positive chunk when disk is plentiful")

	s = autoTuneBalancedProfile{}.Tune(enoughMem, enoughDisk)
	assert.Greater(t, s.chunkSize, int64(0), "Balanced must use positive chunk when disk is plentiful")
}

// TestAutoTuneDiskSpillGuardUnknownDisk verifies that profiles behave normally
// when availDisk is -1 (unknown), i.e. the safety guard is not triggered.
func TestAutoTuneDiskSpillGuardUnknownDisk(t *testing.T) {
	s := autoTuneMemoryProfile{}.Tune(8<<30, -1)
	assert.Greater(t, s.chunkSize, int64(0), "MemoryOptimized must spill normally when disk is unknown")

	s = autoTuneBalancedProfile{}.Tune(8<<30, -1)
	assert.Greater(t, s.chunkSize, int64(0), "Balanced must spill normally when disk is unknown")
}
