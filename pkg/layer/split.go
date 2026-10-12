package layer

import (
	"fmt"
	"path"
	"strings"
)

// Rule is a type-provided hint applied on top of the generic splitting rules.
type Rule struct {
	Prefix   string // Rel subtree this rule applies to, e.g. ".python" or "a/b"
	Separate bool   // the subtree is partitioned and packed in its own layer(s)
	Exclude  bool   // the subtree is dropped entirely
}

// SplitOptions controls automatic layer splitting.
type SplitOptions struct {
	Disabled     bool // one group per partition (Separate rules still apply)
	Large, Small int64
	Rules        []Rule
}

// Group is a set of entries destined for one layer.
type Group struct {
	Name    string // logical group name: rule prefix, or bucket name
	Entries []Entry
	// Part is set by Pack: this group is layer Part of PartsTotal in its slice.
	Part       int
	PartsTotal int
}

// Default thresholds.
const (
	DefaultLargeThreshold = 5 << 30  // 5 GiB
	DefaultSmallThreshold = 64 << 20 // 64 MiB
)

func matchPrefix(rel, prefix string) bool {
	return rel == prefix || strings.HasPrefix(rel, prefix+"/")
}

// Split partitions and bins entries per DESIGN 2.6.
// Entries must be sorted by Rel (as returned by Scan).
func Split(entries []Entry, opts SplitOptions) []Group {
	if opts.Large <= 0 {
		opts.Large = DefaultLargeThreshold
	}
	if opts.Small <= 0 {
		opts.Small = DefaultSmallThreshold
	}
	// Apply exclude rules.
	var kept []Entry
	for _, e := range entries {
		excluded := false
		for _, r := range opts.Rules {
			if r.Exclude && matchPrefix(e.Rel, r.Prefix) {
				excluded = true
				break
			}
		}
		if !excluded {
			kept = append(kept, e)
		}
	}
	// Partition: each Separate rule (in order) owns a partition; remainder last.
	type partition struct {
		name    string
		entries []Entry
	}
	var parts []partition
	rest := kept
	for _, r := range opts.Rules {
		if !r.Separate {
			continue
		}
		var mine, others []Entry
		for _, e := range rest {
			if matchPrefix(e.Rel, r.Prefix) {
				mine = append(mine, e)
			} else {
				others = append(others, e)
			}
		}
		if len(mine) > 0 {
			parts = append(parts, partition{name: r.Prefix, entries: mine})
		}
		rest = others
	}
	if len(rest) > 0 {
		parts = append(parts, partition{name: "", entries: rest})
	}

	var groups []Group
	for _, p := range parts {
		gs := binPartition(p.entries, opts)
		for i := range gs {
			if gs[i].Name == "" {
				gs[i].Name = p.name
			} else if p.name != "" {
				gs[i].Name = p.name + "/" + gs[i].Name
			}
		}
		groups = append(groups, gs...)
	}
	for i := range groups {
		groups[i].Part = i + 1
		groups[i].PartsTotal = len(groups)
	}
	return groups
}

func binPartition(entries []Entry, opts SplitOptions) []Group {
	if opts.Disabled {
		if len(entries) == 0 {
			return nil
		}
		return []Group{{Entries: entries}}
	}
	var large, medium, small []Entry
	for _, e := range entries {
		switch {
		case !e.IsRegular():
			small = append(small, e) // dirs, symlinks
		case e.Size >= opts.Large:
			large = append(large, e)
		case e.Size >= opts.Small:
			medium = append(medium, e)
		default:
			small = append(small, e)
		}
	}
	var groups []Group
	add := func(name string, es []Entry) {
		if len(es) > 0 {
			groups = append(groups, Group{Name: name, Entries: es})
		}
	}
	for _, e := range large {
		add("", []Entry{e})
	}
	for i, bin := range binPack(medium, opts.Large) {
		add(fmt.Sprintf("medium-%d", i+1), bin)
	}
	for i, bin := range binPack(small, opts.Large) {
		add(fmt.Sprintf("small-%d", i+1), bin)
	}
	return groups
}

// binPack greedily bins entries (already in path order) into bins whose
// total size does not exceed cap.
func binPack(entries []Entry, cap int64) [][]Entry {
	var bins [][]Entry
	var cur []Entry
	var size int64
	for _, e := range entries {
		if len(cur) > 0 && size+e.Size > cap {
			bins = append(bins, cur)
			cur, size = nil, 0
		}
		cur = append(cur, e)
		size += e.Size
	}
	if len(cur) > 0 {
		bins = append(bins, cur)
	}
	return bins
}

// ParentDirs returns the ancestor directories (relative to root, slash paths)
// of rel, ordered shallowest first. rel itself is not included.
func ParentDirs(rel string) []string {
	var dirs []string
	for d := path.Dir(rel); d != "." && d != "/"; d = path.Dir(d) {
		dirs = append([]string{d}, dirs...)
	}
	return dirs
}
