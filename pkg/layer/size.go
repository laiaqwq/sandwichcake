package layer

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseSize parses sizes like "5Gi", "64Mi", "1024", "1.5G".
// Binary suffixes (Ki, Mi, Gi, Ti) are powers of 1024; decimal suffixes
// (K, M, G, T and KB, MB, GB, TB) are powers of 1000.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("layer: empty size")
	}
	mult := int64(1)
	lower := strings.ToLower(s)
	suffixes := []struct {
		s string
		m int64
	}{
		{"kib", 1 << 10}, {"mib", 1 << 20}, {"gib", 1 << 30}, {"tib", 1 << 40},
		{"ki", 1 << 10}, {"mi", 1 << 20}, {"gi", 1 << 30}, {"ti", 1 << 40},
		{"kb", 1000}, {"mb", 1000_000}, {"gb", 1000_000_000}, {"tb", 1000_000_000_000},
		{"k", 1000}, {"m", 1000_000}, {"g", 1000_000_000}, {"t", 1000_000_000_000},
		{"b", 1},
	}
	num := s
	for _, sf := range suffixes {
		if strings.HasSuffix(lower, sf.s) {
			mult = sf.m
			num = s[:len(s)-len(sf.s)]
			break
		}
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(num), 64)
	if err != nil {
		return 0, fmt.Errorf("layer: invalid size %q", s)
	}
	if f < 0 {
		return 0, fmt.Errorf("layer: negative size %q", s)
	}
	return int64(f * float64(mult)), nil
}
