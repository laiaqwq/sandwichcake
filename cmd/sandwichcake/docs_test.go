package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const cliDocsDir = "../../docs/content/docs/cli"

// cliDocPage maps a command path (without the root) to its reference page:
// cli/<cmd>.md, cli/<group>/_index.md for groups with their own directory,
// cli/<group>/<sub>.md inside such a directory, else the parent's page
// (e.g. every config subcommand lives on cli/config.md).
func cliDocPage(path []string) string {
	dir := filepath.Join(cliDocsDir, path[0])
	_, err := os.Stat(dir)
	grouped := err == nil
	switch {
	case len(path) == 1 && grouped:
		return filepath.Join(dir, "_index.md")
	case len(path) == 1:
		return dir + ".md"
	case grouped:
		return filepath.Join(dir, path[len(path)-1]+".md")
	default:
		return dir + ".md"
	}
}

func mentionsFlag(doc, name string) bool {
	return regexp.MustCompile(`(^|[^\w-])--` + regexp.QuoteMeta(name) + `([^\w-]|$)`).MatchString(doc)
}

func readDoc(t *testing.T, path string, cache map[string]string) (string, bool) {
	t.Helper()
	if doc, ok := cache[path]; ok {
		return doc, true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	cache[path] = string(data)
	return cache[path], true
}

// TestCLIDocs keeps docs/content/docs/cli in sync with the cobra tree:
// every command needs a page that names it and mentions each of its flags.
func TestCLIDocs(t *testing.T) {
	if _, err := os.Stat(cliDocsDir); err != nil {
		t.Skip("docs not present")
	}
	root := newRoot()
	root.InitDefaultCompletionCmd()
	cache := map[string]string{}

	index, ok := readDoc(t, filepath.Join(cliDocsDir, "_index.md"), cache)
	if !ok {
		t.Fatal("missing cli/_index.md")
	}
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if !mentionsFlag(index, f.Name) {
			t.Errorf("cli/_index.md: global flag --%s not documented", f.Name)
		}
	})

	var walk func(c *cobra.Command, path []string)
	walk = func(c *cobra.Command, path []string) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" {
				continue
			}
			p := append(append([]string{}, path...), sub.Name())
			page := cliDocPage(p)
			rel, _ := filepath.Rel(cliDocsDir, page)
			doc, ok := readDoc(t, page, cache)
			if !ok {
				t.Errorf("%s: no reference page (want cli/%s)", strings.Join(p, " "), filepath.ToSlash(rel))
				walk(sub, p)
				continue
			}
			if usage := "sandwichcake " + strings.Join(p, " "); !strings.Contains(doc, usage) {
				t.Errorf("cli/%s: does not mention %q", filepath.ToSlash(rel), usage)
			}
			// Flags shared by a group's subcommands may be documented once on
			// the group page (cli/pack/_index.md).
			var groupDoc string
			if len(p) > 1 {
				groupDoc, _ = readDoc(t, cliDocPage(p[:1]), cache)
			}
			sub.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
				if f.Name == "help" || mentionsFlag(doc, f.Name) || mentionsFlag(groupDoc, f.Name) {
					return
				}
				t.Errorf("cli/%s: flag --%s of %q not documented", filepath.ToSlash(rel), f.Name, strings.Join(p, " "))
			})
			walk(sub, p)
		}
	}
	walk(root, nil)
}

func TestCLIDocsMentionsFlag(t *testing.T) {
	doc := "| `--to <ref>` | x |\nuse --to-prefix or --no-split."
	for name, want := range map[string]bool{"to": true, "to-prefix": true, "no-split": true, "split": false, "prefix": false} {
		if got := mentionsFlag(doc, name); got != want {
			t.Errorf("mentionsFlag(%q) = %v, want %v", name, got, want)
		}
	}
}
