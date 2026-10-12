// Package elfscan extracts architecture, interpreter, and glibc version
// requirements from ELF binaries.
package elfscan

import (
	"debug/elf"
	"regexp"
	"strconv"
	"strings"
)

var glibcVer = regexp.MustCompile(`^GLIBC_(\d+)\.(\d+)$`)

// Info is the result of scanning one ELF file.
type Info struct {
	Arch     string // GOARCH-style: amd64, arm64, 386, arm, ppc64le, s390x, riscv64
	Interp   string // PT_INTERP, "" if static
	GlibcMin [2]int // max GLIBC_x.y required; [0,0] if none
	Musl     bool   // interp references musl
}

// ScanFile inspects path if it is an ELF file; ok is false for non-ELF.
func ScanFile(path string) (info Info, ok bool, err error) {
	f, err := elf.Open(path)
	if err != nil {
		if err == elf.ErrNoSymbols || isNotELF(err) {
			return Info{}, false, nil
		}
		return Info{}, false, nil // treat unreadable/non-elf as not-elf
	}
	defer f.Close()

	info.Arch = archString(f.Machine)
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			b := make([]byte, p.Filesz)
			if _, err := p.ReadAt(b, 0); err == nil {
				info.Interp = strings.TrimRight(string(b), "\x00")
			}
		}
	}
	info.Musl = strings.Contains(info.Interp, "musl")

	vers := map[[2]int]bool{}
	if syms, err := f.ImportedSymbols(); err == nil {
		for _, s := range syms {
			if m := glibcVer.FindStringSubmatch(s.Version); m != nil {
				vers[[2]int{atoi(m[1]), atoi(m[2])}] = true
			}
		}
	}
	if needs, err := f.DynamicVersionNeeds(); err == nil {
		for _, n := range needs {
			for _, dep := range n.Needs {
				if m := glibcVer.FindStringSubmatch(dep.Dep); m != nil {
					vers[[2]int{atoi(m[1]), atoi(m[2])}] = true
				}
			}
		}
	}
	for v := range vers {
		if v[0] > info.GlibcMin[0] || (v[0] == info.GlibcMin[0] && v[1] > info.GlibcMin[1]) {
			info.GlibcMin = v
		}
	}
	return info, true, nil
}

func isNotELF(err error) bool {
	return strings.Contains(err.Error(), "bad magic number")
}

func archString(m elf.Machine) string {
	switch m {
	case elf.EM_X86_64:
		return "amd64"
	case elf.EM_AARCH64:
		return "arm64"
	case elf.EM_386:
		return "386"
	case elf.EM_ARM:
		return "arm"
	case elf.EM_PPC64:
		return "ppc64le"
	case elf.EM_S390:
		return "s390x"
	case elf.EM_RISCV:
		return "riscv64"
	default:
		return m.String()
	}
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
