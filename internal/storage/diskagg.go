package storage

// CombinedDiskAggBytes returns summed used and total bytes across mounts (dashboard disk summary).
func CombinedDiskAggBytes(disks []DiskSample) (used, total uint64) {
	for _, d := range disks {
		used += d.Used
		total += d.Total
	}
	return used, total
}

// CombinedDiskUsagePct returns the dashboard-style disk usage percentage:
// 100 * sum(used) / sum(total) across all mounts. Matches the main UI disk summary.
func CombinedDiskUsagePct(disks []DiskSample) float64 {
	totalUsed, totalSize := CombinedDiskAggBytes(disks)
	if totalSize == 0 {
		return 0
	}
	return 100.0 * float64(totalUsed) / float64(totalSize)
}
