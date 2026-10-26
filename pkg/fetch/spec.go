// Package fetch lists and streams model files from Hugging Face, ModelScope
// and plain HTTP(S) URLs. It never touches OCI registries (see
// pkg/registry) and keeps credentials out of every value it returns.
package fetch

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// Providers.
const (
	HF         = "hf"
	ModelScope = "modelscope"
	HTTP       = "https"
)

// Default endpoints and revisions.
const (
	DefaultHFEndpoint         = "https://huggingface.co"
	DefaultModelScopeEndpoint = "https://www.modelscope.cn"
	DefaultHFRev              = "main"
	DefaultModelScopeRev      = "master"
)

// Endpoints are the API base URLs (mirrors such as hf-mirror.com allowed).
type Endpoints struct {
	HF, ModelScope string
}

func (e Endpoints) hf() string {
	if e.HF == "" {
		return DefaultHFEndpoint
	}
	return strings.TrimSuffix(e.HF, "/")
}

func (e Endpoints) modelscope() string {
	if e.ModelScope == "" {
		return DefaultModelScopeEndpoint
	}
	return strings.TrimSuffix(e.ModelScope, "/")
}

// Spec is a parsed remote model source.
type Spec struct {
	Provider string
	Repo     string // org/name (hf, modelscope)
	Rev      string // as written by the user (provider default if omitted)
	Subdir   string // only files under this repo path
	File     string // a single repo file
	URL      string // plain URL (Provider == HTTP), fragment stripped
	SHA256   string // expected digest of a plain URL (#sha256=<hex>)
}

// IsRemote reports whether arg names a remote source rather than a dir.
func IsRemote(arg string) bool {
	for _, p := range []string{"hf://", "modelscope://", "https://", "http://"} {
		if strings.HasPrefix(arg, p) {
			return true
		}
	}
	return false
}

// Canonical is the provenance string recorded in annotations:
// hf://org/name@rev, modelscope://org/name@rev, or the URL without query,
// fragment or userinfo (which may carry credentials).
func (s Spec) Canonical() string {
	if s.Provider == HTTP {
		u, err := url.Parse(s.URL)
		if err != nil {
			return ""
		}
		u.User, u.RawQuery, u.Fragment = nil, "", ""
		return u.String()
	}
	return s.Provider + "://" + s.Repo + "@" + s.Rev
}

// Name is a registry-friendly name: org--name, or the file base name.
func (s Spec) Name() string {
	if s.Provider == HTTP {
		u, _ := url.Parse(s.URL)
		if u == nil {
			return "model"
		}
		return path.Base(u.Path)
	}
	return strings.ReplaceAll(s.Repo, "/", "--")
}

// Parse accepts hf://org/name[@rev], modelscope://org/name[@rev],
// https://huggingface.co/org/name[/tree/<rev>[/<dir>] | /blob|resolve/<rev>/<file>],
// https://modelscope.cn/models/org/name[/files | /resolve/<rev>/<file>][?Revision=],
// URLs on the configured endpoint hosts, and any other http(s) URL as a
// single file (optionally with #sha256=<hex>).
func Parse(raw string, ep Endpoints) (Spec, error) {
	for _, p := range []string{HF, ModelScope} {
		if rest, ok := strings.CutPrefix(raw, p+"://"); ok {
			id, rev, _ := strings.Cut(rest, "@")
			s := Spec{Provider: p, Repo: id, Rev: rev}
			return s, s.finish()
		}
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return Spec{}, fmt.Errorf("fetch: unsupported source %q", raw)
	}
	segs, err := pathSegments(u)
	if err != nil {
		return Spec{}, err
	}
	switch {
	case isHost(u.Host, ep.hf(), "huggingface.co", "hf.co"):
		return parseHF(raw, segs)
	case isHost(u.Host, ep.modelscope(), "modelscope.cn", "www.modelscope.cn"):
		return parseModelScope(raw, u, segs)
	}
	s := Spec{Provider: HTTP}
	if frag := u.Fragment; frag != "" {
		sum, ok := strings.CutPrefix(frag, "sha256=")
		if !ok || !isHex(sum, 64) {
			return Spec{}, fmt.Errorf("fetch: fragment must be #sha256=<64 hex>, got %q", frag)
		}
		s.SHA256 = strings.ToLower(sum)
	}
	u.Fragment = ""
	s.URL = u.String()
	if len(segs) == 0 || segs[len(segs)-1] == "" {
		return Spec{}, fmt.Errorf("fetch: URL %q does not name a file", raw)
	}
	return s, nil
}

func parseHF(raw string, segs []string) (Spec, error) {
	if len(segs) > 0 && (segs[0] == "datasets" || segs[0] == "spaces") {
		return Spec{}, fmt.Errorf("fetch: only Hugging Face models are supported, got %q", raw)
	}
	if len(segs) < 2 {
		return Spec{}, fmt.Errorf("fetch: %q is not a model URL (want /org/name)", raw)
	}
	s := Spec{Provider: HF, Repo: segs[0] + "/" + segs[1]}
	rest := segs[2:]
	if len(rest) > 0 {
		if len(rest) < 2 {
			return Spec{}, fmt.Errorf("fetch: cannot parse %q", raw)
		}
		s.Rev = rest[1]
		sub := strings.Join(rest[2:], "/")
		switch rest[0] {
		case "tree":
			s.Subdir = strings.Trim(sub, "/")
		case "blob", "resolve":
			if sub == "" {
				return Spec{}, fmt.Errorf("fetch: %q names no file", raw)
			}
			s.File = sub
		default:
			return Spec{}, fmt.Errorf("fetch: cannot parse %q (want /tree/, /blob/ or /resolve/)", raw)
		}
	}
	return s, s.finish()
}

func parseModelScope(raw string, u *url.URL, segs []string) (Spec, error) {
	if len(segs) < 3 || segs[0] != "models" {
		return Spec{}, fmt.Errorf("fetch: %q is not a ModelScope model URL (want /models/org/name)", raw)
	}
	s := Spec{Provider: ModelScope, Repo: segs[1] + "/" + segs[2], Rev: u.Query().Get("Revision")}
	rest := segs[3:]
	switch {
	case len(rest) == 0 || (len(rest) == 1 && (rest[0] == "files" || rest[0] == "summary" || rest[0] == "")):
	case len(rest) >= 3 && rest[0] == "resolve":
		s.Rev, s.File = rest[1], strings.Join(rest[2:], "/")
	default:
		return Spec{}, fmt.Errorf("fetch: cannot parse %q", raw)
	}
	return s, s.finish()
}

func (s *Spec) finish() error {
	org, name, ok := strings.Cut(s.Repo, "/")
	if !ok || !validSeg(org) || !validSeg(name) || strings.Contains(name, "/") {
		return fmt.Errorf("fetch: invalid %s model id %q (want org/name)", s.Provider, s.Repo)
	}
	if s.Rev == "" {
		s.Rev = DefaultHFRev
		if s.Provider == ModelScope {
			s.Rev = DefaultModelScopeRev
		}
	}
	for _, p := range []string{s.Subdir, s.File} {
		if p != "" && !safeRel(p) {
			return fmt.Errorf("fetch: unsafe path %q", p)
		}
	}
	return nil
}

func validSeg(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, "@:\\")
}

func safeRel(p string) bool {
	if strings.HasPrefix(p, "/") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// pathSegments splits the escaped path so that %2F inside a segment (e.g.
// a refs/pr/1 revision) stays within that segment.
func pathSegments(u *url.URL) ([]string, error) {
	p := strings.Trim(u.EscapedPath(), "/")
	if p == "" {
		return nil, nil
	}
	var out []string
	for _, seg := range strings.Split(p, "/") {
		v, err := url.PathUnescape(seg)
		if err != nil {
			return nil, fmt.Errorf("fetch: bad path in %s: %w", u, err)
		}
		out = append(out, v)
	}
	return out, nil
}

func isHost(host, endpoint string, extra ...string) bool {
	if u, err := url.Parse(endpoint); err == nil && u.Host != "" && strings.EqualFold(u.Host, host) {
		return true
	}
	for _, h := range extra {
		if strings.EqualFold(h, host) {
			return true
		}
	}
	return false
}

func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, c := range strings.ToLower(s) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
