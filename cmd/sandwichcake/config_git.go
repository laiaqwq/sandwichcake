package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/laiaqwq/sandwichcake/internal/cache"
)

type gitSource struct {
	URL, Branch, Subdir string
}

func parseGitSource(source string) (gitSource, error) {
	var s gitSource
	if !strings.HasPrefix(source, "git+https://") && !strings.HasPrefix(source, "git+ssh://") {
		return s, fmt.Errorf("git source must use git+https:// or git+ssh://")
	}
	remote, subdir, ok := strings.Cut(strings.TrimPrefix(source, "git+"), "#")
	if !ok || !validSyncPath(subdir) {
		return s, fmt.Errorf("git source requires a safe #subdir (cannot sync a whole repository)")
	}
	idx := strings.LastIndex(remote, ".git@")
	if idx < 0 {
		return s, fmt.Errorf("git source requires .git@<branch>#<subdir>")
	}
	s.URL, s.Branch, s.Subdir = remote[:idx+4], remote[idx+5:], subdir
	parsed, err := url.Parse(s.URL)
	if err != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme == "https" && parsed.User != nil) ||
		(parsed.User != nil && strings.Contains(parsed.User.String(), ":")) {
		return gitSource{}, fmt.Errorf("invalid git URL (credentials must use a credential helper)")
	}
	if s.Branch == "" || strings.HasPrefix(s.Branch, "-") || strings.ContainsAny(s.Branch, " \t\n") {
		return gitSource{}, fmt.Errorf("invalid git branch")
	}
	return s, nil
}

func gitExec(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := gitBytes(ctx, dir, args...)
	return strings.TrimSpace(string(out)), err
}

func gitBytes(ctx context.Context, dir string, args ...string) ([]byte, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git not found; install with xcode-select --install, brew install git, or apt install git")
	}
	cmd := exec.CommandContext(ctx, git, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		var e *exec.ExitError
		if errors.As(err, &e) {
			return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(e.Stderr)))
		}
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return out, nil
}

func gitClone(ctx context.Context, source gitSource) (string, string, error) {
	cfg, _, err := effectiveConfig()
	if err != nil {
		return "", "", err
	}
	base := cfg.Cache.Dir
	if base == "" {
		base = cache.Dir()
	}
	h := sha256.Sum256([]byte(source.URL))
	dir := filepath.Join(base, fmt.Sprintf("project-git-%x", h[:8]))
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", "", err
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		// The clone is disposable cache data; never run git commands in the
		// user's project tree.
		if _, err := gitExec(ctx, "", "clone", "--quiet", "--no-checkout", source.URL, dir); err != nil {
			return "", "", err
		}
	} else if err != nil {
		return "", "", err
	}
	origin, err := gitExec(ctx, dir, "config", "--get", "remote.origin.url")
	if err != nil || origin != source.URL {
		return "", "", fmt.Errorf("cached clone has unexpected origin: %s", dir)
	}
	if _, err := gitExec(ctx, dir, "check-ref-format", "--branch", source.Branch); err != nil {
		return "", "", err
	}
	if _, err := gitExec(ctx, dir, "fetch", "--quiet", "origin", "refs/heads/"+source.Branch); err != nil {
		return "", "", err
	}
	commit, err := gitExec(ctx, dir, "rev-parse", "FETCH_HEAD")
	return dir, commit, err
}

func pullGit(ctx context.Context, root, source string) (map[string][]byte, string, error) {
	ref, err := parseGitSource(source)
	if err != nil {
		return nil, "", err
	}
	dir, commit, err := gitClone(ctx, ref)
	if err != nil {
		return nil, "", err
	}
	tree, err := gitBytes(ctx, dir, "ls-tree", "-r", "-z", commit, "--", ref.Subdir)
	if err != nil {
		return nil, "", err
	}
	type entry struct{ path, mode, hash string }
	var entries []entry
	for _, item := range strings.Split(string(tree), "\x00") {
		if item == "" {
			continue
		}
		meta, full, ok := strings.Cut(item, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || !strings.HasPrefix(full, ref.Subdir+"/") {
			return nil, "", fmt.Errorf("invalid git tree entry")
		}
		rel := strings.TrimPrefix(full, ref.Subdir+"/")
		if !validSyncPath(rel) {
			return nil, "", fmt.Errorf("unsafe git path %q", rel)
		}
		entries = append(entries, entry{rel, fields[0], fields[2]})
	}
	var patterns []string
	for _, e := range entries {
		if e.path == ".cakeignore" {
			if e.mode != "100644" && e.mode != "100755" {
				return nil, "", fmt.Errorf(".cakeignore must be a regular file")
			}
			data, err := gitBytes(ctx, dir, "cat-file", "blob", e.hash)
			if err != nil {
				return nil, "", err
			}
			patterns = strings.Split(string(data), "\n")
			break
		}
	}
	files := map[string][]byte{}
	for _, e := range entries {
		if ignoredSyncPath(e.path, patterns) {
			continue
		}
		if e.mode != "100644" && e.mode != "100755" {
			return nil, "", fmt.Errorf("refusing git symlink or submodule %s", e.path)
		}
		data, err := gitBytes(ctx, dir, "cat-file", "blob", e.hash)
		if err != nil {
			return nil, "", err
		}
		files[e.path] = data
	}
	return files, commit, nil
}

func checkCleanGitIndex(ctx context.Context, dir string) error {
	_, err := gitExec(ctx, dir, "diff", "--cached", "--quiet")
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return fmt.Errorf("cached git clone has staged changes; refusing to include unrelated files in the push")
	}
	return err
}

func pushGit(ctx context.Context, cmd *cobra.Command, root, source, branch string, files map[string][]byte, state syncState) error {
	ref, err := parseGitSource(source)
	if err != nil {
		return err
	}
	if state.Source != source || state.Version == "" {
		return fmt.Errorf("pull this source before pushing to git")
	}
	if !regexp.MustCompile(`^[0-9a-f]{40,64}$`).MatchString(state.Version) {
		return fmt.Errorf("invalid recorded git commit")
	}
	dir, head, err := gitClone(ctx, ref)
	if err != nil {
		return err
	}
	if branch == "" && head != state.Version {
		return fmt.Errorf("remote branch changed (%s != %s); pull first", head, state.Version)
	}
	if branch != "" {
		if _, err := gitExec(ctx, dir, "check-ref-format", "--branch", branch); err != nil {
			return err
		}
		check := exec.CommandContext(ctx, "git", "ls-remote", "--exit-code", "--heads", "origin", "refs/heads/"+branch)
		check.Dir = dir
		out, err := check.CombinedOutput()
		if err == nil || check.ProcessState == nil || check.ProcessState.ExitCode() != 2 {
			return fmt.Errorf("cannot create branch %s: %v %s", branch, err, out)
		}
	}
	target := ref.Branch
	if branch != "" {
		target = branch
	}
	if _, err := gitExec(ctx, dir, "checkout", "--quiet", "-B", target, state.Version); err != nil {
		return err
	}
	if err := checkCleanGitIndex(ctx, dir); err != nil {
		return err
	}
	tree := filepath.Join(dir, filepath.FromSlash(ref.Subdir))
	// Only delete files tracked in the previously pulled project snapshot.
	// Unrelated remote files elsewhere in the repository remain untouched.
	for p := range state.Files {
		if _, ok := files[p]; ok {
			continue
		}
		if err := checkSyncParents(tree, p); err != nil {
			return err
		}
		full := filepath.Join(tree, filepath.FromSlash(p))
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for _, p := range sortedFiles(files) {
		full := filepath.Join(tree, filepath.FromSlash(p))
		for parent := filepath.Dir(full); strings.HasPrefix(parent, tree) && parent != tree; parent = filepath.Dir(parent) {
			if info, err := os.Lstat(parent); err == nil && !info.IsDir() {
				return fmt.Errorf("unsafe git path %s", p)
			}
		}
		if info, err := os.Lstat(full); err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe git path %s", p)
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, files[p], 0o644); err != nil {
			return err
		}
	}
	paths := make(map[string]bool, len(files)+len(state.Files))
	for p := range files {
		paths[p] = true
	}
	for p := range state.Files {
		paths[p] = true
	}
	var staged []string
	for p := range paths {
		staged = append(staged, path.Join(ref.Subdir, p))
	}
	sort.Strings(staged)
	for len(staged) > 0 {
		n := len(staged)
		if n > 128 {
			n = 128
		}
		if _, err := gitExec(ctx, dir, append([]string{"add", "-f", "-A", "--"}, staged[:n]...)...); err != nil {
			return err
		}
		staged = staged[n:]
	}
	if _, err := gitExec(ctx, dir, "diff", "--cached", "--quiet"); err == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "no changes")
		return nil
	}
	if _, err := gitExec(ctx, dir, "commit", "-m", "Update sandwichcake project"); err != nil {
		return err
	}
	if _, err := gitExec(ctx, dir, "push", "origin", "HEAD:refs/heads/"+target); err != nil {
		return err
	}
	commit, err := gitExec(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "pushed %s at %s\n", target, commit)
	if branch == "" {
		return saveState(root, syncState{Source: source, Version: commit, Files: syncHashes(files)})
	}
	return nil
}
