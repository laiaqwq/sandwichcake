package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/laiaqwq/sandwichcake/internal/cache"
)

type syncState struct {
	Source  string            `json:"source"`
	Version string            `json:"version"`
	Files   map[string]string `json:"files"`
}

func stateFile(root string) string { return filepath.Join(root, ".cake", "state.json") }

func readState(root string) (syncState, error) {
	var s syncState
	data, err := os.ReadFile(stateFile(root))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	for p := range s.Files {
		if !validSyncPath(p) {
			return s, fmt.Errorf("unsafe path %q in sync state", p)
		}
	}
	return s, nil
}

func saveState(root string, s syncState) error {
	if err := os.MkdirAll(filepath.Join(root, ".cake"), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(stateFile(root), append(data, '\n'), 0o600)
}

func fileHash(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}

// Paths are slash separated, relative, regular files only. Symlinks are
// rejected instead of followed to prevent leaking files outside the project.
func validSyncPath(p string) bool {
	if p == "" || p == "." || strings.Contains(p, "\\") || path.IsAbs(p) || path.Clean(p) != p || strings.HasPrefix(p, "../") || p == ".." {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." || part == ".git" || part == ".cake" || part == "cake-config.local.yaml" || strings.HasSuffix(part, ".env") {
			return false
		}
	}
	return true
}

func ignoredSyncPath(rel string, patterns []string) bool {
	ignored := false
	for _, raw := range patterns {
		p := strings.TrimRight(raw, " \r")
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		negate := strings.HasPrefix(p, "!")
		if negate {
			p = p[1:]
		}
		if strings.HasPrefix(p, `\!`) || strings.HasPrefix(p, `\#`) {
			p = p[1:]
		}
		p = strings.TrimSuffix(p, "/")
		if p == "" {
			continue
		}
		if ignoreMatch(rel, p) {
			ignored = !negate
		}
	}
	return ignored
}

func ignoreMatch(rel, pattern string) bool {
	// Support the common gitignore globs (*, ?, **, []), directory exclusions,
	// root anchors and last-match-wins negation. Safety exclusions are
	// checked separately by validSyncPath and cannot be negated.
	var b strings.Builder
	anchored := strings.HasPrefix(pattern, "/")
	pattern = strings.TrimPrefix(pattern, "/")
	if anchored || strings.Contains(pattern, "/") {
		b.WriteString("^")
	} else {
		b.WriteString("(^|/)")
	}
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '*':
			if i+1 < len(runes) && runes[i+1] == '*' {
				i++
				if i+1 < len(runes) && runes[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '[':
			class, end, ok := ignoreClass(runes, i)
			if ok {
				b.WriteString(class)
				i = end
			} else {
				b.WriteString(`\[`)
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(runes[i])))
		}
	}
	b.WriteString("($|/)")
	re, err := regexp.Compile(b.String())
	return err == nil && re.MatchString(rel)
}

func ignoreClass(pattern []rune, start int) (string, int, bool) {
	i := start + 1
	negated := i < len(pattern) && pattern[i] == '!'
	if negated {
		i++
	}
	first := i
	if i < len(pattern) && pattern[i] == ']' {
		i++ // A leading ']' is literal inside a character class.
	}
	for ; i < len(pattern); i++ {
		if pattern[i] == '[' && i+1 < len(pattern) && pattern[i+1] == ':' {
			j := i + 2
			for j+1 < len(pattern) && !(pattern[j] == ':' && pattern[j+1] == ']') {
				j++
			}
			if j+1 < len(pattern) {
				i = j + 1
				continue
			}
		}
		if pattern[i] == ']' && i > first {
			body := string(pattern[start+1 : i])
			if negated {
				body = "^" + string(pattern[start+2:i])
			}
			if strings.ContainsAny(body, "/\\") {
				return "", 0, false
			}
			class := "[" + body + "]"
			if _, err := regexp.Compile(class); err != nil {
				return "", 0, false
			}
			return class, i, true
		}
	}
	return "", 0, false
}

func projectFiles(root string) (map[string][]byte, error) {
	cfg, _, err := effectiveConfig()
	if err != nil {
		return nil, err
	}
	cacheDir := cfg.Cache.Dir
	if cacheDir == "" {
		cacheDir = cache.Dir()
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	cacheAbs, err := filepath.Abs(cacheDir)
	if err != nil {
		return nil, err
	}
	cacheRel, err := filepath.Rel(rootAbs, cacheAbs)
	if err != nil {
		return nil, err
	}
	if cacheRel == "." {
		return nil, fmt.Errorf("cache directory cannot be the project root")
	}
	var patterns []string
	data, err := os.ReadFile(filepath.Join(root, ".cakeignore"))
	if err == nil {
		patterns = strings.Split(string(data), "\n")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	files := map[string][]byte{}
	err = filepath.WalkDir(root, func(full string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if full == root {
			return nil
		}
		rel, err := filepath.Rel(root, full)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == filepath.ToSlash(cacheRel) || strings.HasPrefix(rel, filepath.ToSlash(cacheRel)+"/") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !validSyncPath(rel) || ignoredSyncPath(rel, patterns) {
			if d.IsDir() && (!validSyncPath(rel) || !hasNegations(patterns)) {
				return filepath.SkipDir
			}
			if !d.IsDir() {
				return nil
			}
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to sync symlink %s", rel)
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("refusing to sync non-regular file %s", rel)
		}
		b, err := os.ReadFile(full)
		if err == nil {
			files[rel] = b
		}
		return err
	})
	return files, err
}

func hasNegations(patterns []string) bool {
	for _, p := range patterns {
		if strings.HasPrefix(p, "!") {
			return true
		}
	}
	return false
}

func syncHashes(files map[string][]byte) map[string]string {
	hashes := make(map[string]string, len(files))
	for p, data := range files {
		hashes[p] = fileHash(data)
	}
	return hashes
}

func validateIncoming(files map[string][]byte) error {
	for p := range files {
		if !validSyncPath(p) {
			return fmt.Errorf("unsafe incoming path %q", p)
		}
		if p == ".cakeignore" {
			continue
		}
	}
	config, ok := files["cake-config.yaml"]
	if !ok {
		return fmt.Errorf("remote project has no cake-config.yaml")
	}
	var p projectConfig
	dec := yaml.NewDecoder(bytes.NewReader(config))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return fmt.Errorf("cake-config.yaml: %w", err)
	}
	if p.APIVersion != "sandwichcake.io/v1" || p.Kind != "CakeConfig" {
		return fmt.Errorf("cake-config.yaml: invalid apiVersion or kind")
	}
	return nil
}

func applyPull(cmd *cobra.Command, root, source, version string, files map[string][]byte, force bool) error {
	if err := validateIncoming(files); err != nil {
		return err
	}
	state, err := readState(root)
	if err != nil {
		return err
	}
	if state.Source != "" && state.Source != source && !force {
		return fmt.Errorf("source changed from %s; use --force", state.Source)
	}
	local := map[string][]byte{}
	touched := map[string]bool{}
	for p := range state.Files {
		touched[p] = true
	}
	for p := range files {
		touched[p] = true
	}
	// Read touched paths directly: local .cakeignore must not conceal a
	// modified file that an earlier pull installed.
	for p := range touched {
		if err := checkSyncParents(root, p); err != nil {
			return err
		}
		full := filepath.Join(root, filepath.FromSlash(p))
		info, err := os.Lstat(full)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace non-regular file %s", p)
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		local[p] = data
	}
	// Only remove paths the previous pull installed. Unrelated local files
	// never disappear. Detect modifications before making any writes.
	for p, hash := range state.Files {
		if b, ok := local[p]; ok && fileHash(b) != hash && !force {
			return fmt.Errorf("locally modified %s; use --force", p)
		}
	}
	for p := range files {
		if old, exists := local[p]; exists && !force {
			_, tracked := state.Files[p]
			scaffold := p == "cake-config.yaml" && string(old) == projectScaffold ||
				p == ".gitignore" && string(old) == "cake-config.local.yaml\n.cake/\n"
			if !tracked && !scaffold {
				return fmt.Errorf("untracked local file %s; use --force", p)
			}
		}
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := checkSyncParents(root, p); err != nil {
			return err
		}
		if info, err := os.Lstat(full); err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("refusing to replace non-regular file %s", p)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	for p := range state.Files {
		if _, ok := files[p]; !ok {
			if err := checkSyncParents(root, p); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", p)
		}
	}
	keys := sortedFiles(files)
	for _, p := range keys {
		if fileHash(local[p]) != fileHash(files[p]) {
			fmt.Fprintf(cmd.OutOrStdout(), "+ %s\n", p)
		}
	}
	for p := range state.Files {
		if _, ok := files[p]; !ok {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(p))); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	for _, p := range keys {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, files[p], 0o644); err != nil {
			return err
		}
	}
	return saveState(root, syncState{Source: source, Version: version, Files: syncHashes(files)})
}

func checkSyncParents(root, p string) error {
	for parent := filepath.Dir(filepath.Join(root, filepath.FromSlash(p))); parent != root; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err == nil && !info.IsDir() {
			return fmt.Errorf("unsafe parent directory for %s", p)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func sortedFiles(files map[string][]byte) []string {
	keys := make([]string, 0, len(files))
	for p := range files {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	return keys
}

func syncSource(args []string, root string) (string, error) {
	if len(args) != 0 {
		return args[0], nil
	}
	cfg, err := os.ReadFile(filepath.Join(root, "cake-config.yaml"))
	if err != nil {
		return "", fmt.Errorf("supply a source or initialize a project: %w", err)
	}
	var source struct {
		From string `yaml:"from"`
	}
	if err := yaml.Unmarshal(cfg, &source); err != nil {
		return "", err
	}
	if source.From == "" {
		return "", fmt.Errorf("missing source: pass an address or set from in cake-config.yaml")
	}
	return source.From, nil
}

func syncRoot() (string, error) {
	dir, err := currentProject()
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", fmt.Errorf("no project found; run sandwichcake init or pass --project")
	}
	return dir, nil
}

func configPullCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{Use: "pull [source]", Short: "Pull a project from OCI or git", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := syncRoot()
			if err != nil {
				return err
			}
			source, err := syncSource(args, root)
			if err != nil {
				return err
			}
			var files map[string][]byte
			var version string
			if strings.HasPrefix(source, "oci://") {
				files, version, err = pullOCI(cmd.Context(), source)
			} else if strings.HasPrefix(source, "git+") {
				files, version, err = pullGit(cmd.Context(), root, source)
			} else {
				return fmt.Errorf("source must start with oci:// or git+")
			}
			if err != nil {
				return err
			}
			if err := os.MkdirAll(root, 0o755); err != nil {
				return err
			}
			return applyPull(cmd, root, source, version, files, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace locally modified files")
	return cmd
}

func configPushCmd() *cobra.Command {
	var force bool
	var branch string
	cmd := &cobra.Command{Use: "push [destination]", Short: "Push the project to OCI or git", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := syncRoot()
			if err != nil {
				return err
			}
			source, err := syncSource(args, root)
			if err != nil {
				return err
			}
			files, err := projectFiles(root)
			if err != nil {
				return err
			}
			if err := validateIncoming(files); err != nil {
				return err
			}
			state, err := readState(root)
			if err != nil {
				return err
			}
			if strings.HasPrefix(source, "git+") {
				if force {
					return fmt.Errorf("--force is not supported for git pushes")
				}
				return pushGit(cmd.Context(), cmd, root, source, branch, files, state)
			}
			if branch != "" {
				return fmt.Errorf("--new-branch is only supported for git")
			}
			if !strings.HasPrefix(source, "oci://") {
				return fmt.Errorf("destination must start with oci:// or git+")
			}
			version, err := pushOCI(cmd.Context(), source, files, state, force)
			if err != nil {
				return err
			}
			return saveState(root, syncState{Source: source, Version: version, Files: syncHashes(files)})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "override changed OCI tag")
	cmd.Flags().StringVar(&branch, "new-branch", "", "publish a new git branch rather than updating the source branch")
	return cmd
}
