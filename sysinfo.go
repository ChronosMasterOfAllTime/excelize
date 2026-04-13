// Copyright 2016 - 2026 The excelize Authors. All rights reserved. Use of
// this source code is governed by a BSD-style license that can be found in
// the LICENSE file.

package excelize

import (
	"os"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

// availableMemoryBytes returns the available system memory in bytes.
// It delegates to gopsutil which handles Linux, macOS, Windows, FreeBSD,
// and other platforms correctly using each OS's native APIs.
//
// The Available field in VirtualMemoryStat is computed from kernel-specific
// values and includes reclaimable cache and buffer memory, not just free
// pages — it accurately reflects what a new allocation can use.
func availableMemoryBytes() int64 {
	v, err := mem.VirtualMemory()
	if err != nil || v == nil || v.Available == 0 {
		return autoTuneFallbackMem
	}
	return int64(v.Available)
}

// availableDiskBytes returns the free space in the OS temp directory in bytes.
// Returns -1 when the query fails, which callers must treat as "unknown".
func availableDiskBytes() int64 {
	d, err := disk.Usage(os.TempDir())
	if err != nil || d == nil {
		return -1
	}
	return int64(d.Free)
}
