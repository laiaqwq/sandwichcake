package types

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var glibcReq = regexp.MustCompile(`^glibc\s*>=\s*(\d+)\.(\d+)$`)

// CheckRequires validates a layer.requires value ("glibc>=X.Y", comma
// separated) against the compose context. Unknown requirement kinds produce
// warnings; an unknown base libc produces warnings for libc requirements.
func CheckRequires(requires string, ctx ComposeCtx) []Finding {
	var out []Finding
	for _, req := range strings.Split(requires, ",") {
		req = strings.TrimSpace(req)
		if req == "" {
			continue
		}
		m := glibcReq.FindStringSubmatch(req)
		if m == nil {
			out = append(out, Finding{Warn, fmt.Sprintf("unknown requirement %q", req)})
			continue
		}
		need := [2]int{atoi(m[1]), atoi(m[2])}
		if ctx.BaseLibc == "" {
			out = append(out, Finding{Warn, fmt.Sprintf("cannot verify %s: base libc unknown", req)})
			continue
		}
		if strings.HasPrefix(ctx.BaseLibc, "musl") {
			out = append(out, Finding{Error, fmt.Sprintf("%s: base uses musl libc", req)})
			continue
		}
		var have [2]int
		if n, _ := fmt.Sscanf(ctx.BaseLibc, "glibc %d.%d", &have[0], &have[1]); n != 2 {
			out = append(out, Finding{Warn, fmt.Sprintf("cannot verify %s: unparseable base libc %q", req, ctx.BaseLibc)})
			continue
		}
		if CompareVersion(have, need) < 0 {
			out = append(out, Finding{Error, fmt.Sprintf("%s: base has glibc %d.%d", req, have[0], have[1])})
		}
	}
	return out
}

// CompareVersion compares dotted versions a and b.
func CompareVersion(a, b [2]int) int {
	if a[0] != b[0] {
		return a[0] - b[0]
	}
	return a[1] - b[1]
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
