package doctest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	docsDir = "../../docs/content"
	readme  = "../../README.md"
)

var (
	// Markdown body links/images: [text](target), ![alt](src).
	mdLink = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)`)
	// URI schemes (https:, mailto:, hf:, …) and protocol-relative URLs.
	scheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
	// Explicit goldmark heading anchors: {#id}
	explicitAnchor = regexp.MustCompile(`\{#([A-Za-z0-9_-]+)\}`)
	heading        = regexp.MustCompile(`^#{1,6}\s+(.*)`)
)

// anchors returns every linkable anchor in a markdown file: explicit
// {#id} attributes plus slugified headings (goldmark auto IDs).
func anchors(t *testing.T, path string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	inFence := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range explicitAnchor.FindAllStringSubmatch(line, -1) {
			out[m[1]] = true
		}
		if m := heading.FindStringSubmatch(line); m != nil {
			out[slug(m[1])] = true
		}
	}
	return out
}

// slug mimics goldmark's auto heading IDs: lowercase, spaces to
// hyphens, drop anything else.
func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// bodyLinks extracts markdown link targets from content, skipping fenced
// code blocks (shortcode attributes like card link="x" are not markdown
// links and are never matched).
func bodyLinks(content string) []string {
	var links []string
	inFence := false
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range mdLink.FindAllStringSubmatch(line, -1) {
			links = append(links, m[1])
		}
	}
	return links
}

// lintFile checks every local link in the markdown file at path and
// returns the problems found. If strictMD is true, relative links must
// end in .md — Hextra's render-link hook only resolves *.md targets in
// body text, and bare targets render as broken relative URLs.
func lintFile(t *testing.T, path string, strictMD bool) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var problems []string
	for _, link := range bodyLinks(string(data)) {
		if scheme.MatchString(link) || strings.HasPrefix(link, "//") ||
			strings.HasPrefix(link, "#") {
			continue // external URI or same-page anchor
		}
		target, frag, _ := strings.Cut(link, "#")
		if strictMD && !strings.HasSuffix(target, ".md") {
			problems = append(problems,
				fmt.Sprintf("relative body link %q lacks .md suffix", link))
			continue
		}
		full := filepath.Join(filepath.Dir(path), filepath.FromSlash(target))
		if _, err := os.Stat(full); err != nil {
			problems = append(problems,
				fmt.Sprintf("link %q target does not exist", link))
			continue
		}
		if frag != "" && strings.HasSuffix(target, ".md") &&
			!anchors(t, full)[frag] {
			problems = append(problems,
				fmt.Sprintf("link %q fragment not found in target", link))
		}
	}
	return problems
}

func checkFile(t *testing.T, path string, strictMD bool) {
	t.Helper()
	for _, p := range lintFile(t, path, strictMD) {
		t.Errorf("%s: %s", path, p)
	}
}

func TestDocsMarkdownLinks(t *testing.T) {
	if _, err := os.Stat(docsDir); err != nil {
		t.Skip("docs dir not present")
	}
	err := filepath.WalkDir(docsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		checkFile(t, path, true)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReadmeLinks(t *testing.T) {
	if _, err := os.Stat(readme); err != nil {
		t.Skip("README.md not present")
	}
	checkFile(t, readme, false)
}

// TestCheckerCatchesBadLinks exercises the checker itself on a synthetic
// fixture so a dead checker cannot silently pass.
func TestCheckerCatchesBadLinks(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"other.md": "# Other\n\n## Install\n",
		"good.md":  "# ok\n\n[exists](other.md) [anchored](other.md#install) [ext](https://x.y/z) [self](#top)\n",
		"bad.md":   "[nomd](other) [missing](gone.md) [badanchor](other.md#nope)\n```\n[notalink](zzz)\n```\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := lintFile(t, filepath.Join(dir, "good.md"), true); len(got) != 0 {
		t.Errorf("good.md: unexpected problems %v", got)
	}
	if got := lintFile(t, filepath.Join(dir, "bad.md"), true); len(got) != 3 {
		t.Errorf("bad.md: got %d problems %v, want 3", len(got), got)
	}
}
