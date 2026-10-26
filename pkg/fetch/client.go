package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// File is one remote file of a model.
type File struct {
	Path    string // repo-relative slash path
	Size    int64
	SHA256  string // expected hex sha256 ("" = unknown)
	GitSHA1 string // expected git blob sha1 (HF non-LFS files)
	URL     string // download URL
}

// Listing is the resolved file list of a Spec.
type Listing struct {
	Files  []File
	Commit string // resolved immutable revision ("" if unknown)
}

// Options configures a Client.
type Options struct {
	Endpoints   Endpoints
	Transport   http.RoundTripper // default http.DefaultTransport
	Retries     int               // per request (default 5)
	Connections int               // parallel ranged requests per file (default 4)
	SegmentSize int64             // bytes per ranged request (default 32 MiB)
	// Tokens; empty means anonymous. Callers normally pass HFToken() and
	// ModelScopeToken().
	HFToken, ModelScopeToken string
	// Backoff overrides the retry delay (tests).
	Backoff func(attempt int) time.Duration
}

// Client lists and downloads remote files.
type Client struct {
	opts Options
	http *http.Client
}

// New returns a Client.
func New(o Options) *Client {
	if o.Transport == nil {
		o.Transport = http.DefaultTransport
	}
	if o.Retries <= 0 {
		o.Retries = 5
	}
	if o.Connections <= 0 {
		o.Connections = 4
	}
	if o.SegmentSize <= 0 {
		o.SegmentSize = 32 << 20
	}
	if o.Backoff == nil {
		o.Backoff = func(a int) time.Duration {
			d := time.Duration(1<<a) * 500 * time.Millisecond
			if d > 30*time.Second {
				d = 30 * time.Second
			}
			return d
		}
	}
	c := &Client{opts: o}
	c.http = &http.Client{
		Transport: o.Transport,
		// Credentials are only ever sent to the host they belong to; CDN
		// redirects (cdn-lfs, xet bridge, OSS) carry signed URLs instead.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("fetch: too many redirects")
			}
			if !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
	return c
}

func (c *Client) authorize(req *http.Request) {
	host := req.URL.Host
	switch {
	case isHost(host, c.opts.Endpoints.hf(), "huggingface.co", "hf.co"):
		if c.opts.HFToken != "" {
			req.Header.Set("Authorization", "Bearer "+c.opts.HFToken)
		}
	case isHost(host, c.opts.Endpoints.modelscope(), "modelscope.cn", "www.modelscope.cn"):
		if c.opts.ModelScopeToken != "" {
			req.Header.Set("Authorization", "Bearer "+c.opts.ModelScopeToken)
		}
	default:
		if user, pass, ok := netrcLookup(req.URL.Hostname()); ok {
			req.SetBasicAuth(user, pass)
		}
	}
}

// StatusError is a non-success HTTP response.
type StatusError struct {
	URL    string
	Status int
	Hint   string
}

func (e *StatusError) Error() string {
	msg := fmt.Sprintf("fetch: GET %s: %d %s", e.URL, e.Status, http.StatusText(e.Status))
	if e.Hint != "" {
		msg += " (" + e.Hint + ")"
	}
	return msg
}

func retryable(err error) bool {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Status == http.StatusTooManyRequests || se.Status >= 500
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func (c *Client) statusError(u string, code int) error {
	e := &StatusError{URL: redact(u), Status: code}
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		e.Hint = "private or gated model: accept the license on the model page and set HF_TOKEN / MODELSCOPE_API_TOKEN, or add the host to ~/.netrc"
	case http.StatusNotFound:
		e.Hint = "check the model id, revision and path"
	}
	return e
}

// redact drops query and userinfo, which may hold signatures or secrets.
func redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<url>"
	}
	u.User, u.RawQuery = nil, ""
	return u.String()
}

// do performs a GET with retries; rng is a Range header value or "".
func (c *Client) do(ctx context.Context, method, u, rng string) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= c.opts.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.opts.Backoff(attempt)):
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, u, nil)
		if err != nil {
			return nil, err
		}
		c.authorize(req)
		if rng != "" {
			req.Header.Set("Range", rng)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
		} else if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
			return resp, nil
		} else {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
			resp.Body.Close()
			lastErr = c.statusError(u, resp.StatusCode)
		}
		if !retryable(lastErr) {
			break
		}
	}
	return nil, lastErr
}

func (c *Client) getJSON(ctx context.Context, u string, v any) (http.Header, error) {
	resp, err := c.do(ctx, http.MethodGet, u, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return nil, fmt.Errorf("fetch: decoding %s: %w", redact(u), err)
	}
	return resp.Header, nil
}

// List resolves s to its files (sorted by path is not guaranteed).
func (c *Client) List(ctx context.Context, s Spec) (*Listing, error) {
	var l *Listing
	var err error
	switch s.Provider {
	case HF:
		l, err = c.listHF(ctx, s)
	case ModelScope:
		l, err = c.listModelScope(ctx, s)
	case HTTP:
		l, err = c.listHTTP(ctx, s)
	default:
		return nil, fmt.Errorf("fetch: unknown provider %q", s.Provider)
	}
	if err != nil {
		return nil, err
	}
	var kept []File
	for _, f := range l.Files {
		if !safeRel(f.Path) {
			return nil, fmt.Errorf("fetch: remote returned unsafe path %q", f.Path)
		}
		switch {
		case s.File != "" && f.Path != s.File:
		case s.Subdir != "" && !strings.HasPrefix(f.Path, s.Subdir+"/"):
		default:
			kept = append(kept, f)
		}
	}
	if len(kept) == 0 {
		return nil, fmt.Errorf("fetch: no files found for %s", s.Canonical())
	}
	l.Files = kept
	return l, nil
}

func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

func (c *Client) listHF(ctx context.Context, s Spec) (*Listing, error) {
	ep := c.opts.Endpoints.hf()
	var info struct {
		SHA string `json:"sha"`
	}
	if _, err := c.getJSON(ctx, ep+"/api/models/"+s.Repo+"/revision/"+url.PathEscape(s.Rev), &info); err != nil {
		return nil, err
	}
	if info.SHA == "" {
		return nil, fmt.Errorf("fetch: %s: revision %q did not resolve to a commit", s.Repo, s.Rev)
	}
	l := &Listing{Commit: info.SHA}
	next := ep + "/api/models/" + s.Repo + "/tree/" + info.SHA
	if s.Subdir != "" {
		next += "/" + escapePath(s.Subdir)
	}
	next += "?recursive=true"
	for next != "" {
		var items []struct {
			Type string `json:"type"`
			Oid  string `json:"oid"`
			Size int64  `json:"size"`
			Path string `json:"path"`
			LFS  *struct {
				Oid  string `json:"oid"`
				Size int64  `json:"size"`
			} `json:"lfs"`
		}
		hdr, err := c.getJSON(ctx, next, &items)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if it.Type != "file" {
				continue
			}
			f := File{
				Path: it.Path, Size: it.Size,
				URL: ep + "/" + s.Repo + "/resolve/" + info.SHA + "/" + escapePath(it.Path),
			}
			if it.LFS != nil {
				f.SHA256, f.Size = strings.ToLower(it.LFS.Oid), it.LFS.Size
			} else {
				f.GitSHA1 = strings.ToLower(it.Oid)
			}
			l.Files = append(l.Files, f)
		}
		next = nextLink(hdr.Get("Link"), next)
	}
	return l, nil
}

// nextLink extracts rel="next" from an RFC 8288 Link header.
func nextLink(h, base string) string {
	for _, part := range strings.Split(h, ",") {
		target, params, ok := strings.Cut(strings.TrimSpace(part), ";")
		if !ok || !strings.Contains(strings.ReplaceAll(params, " ", ""), `rel="next"`) {
			continue
		}
		target = strings.Trim(strings.TrimSpace(target), "<>")
		b, err := url.Parse(base)
		t, err2 := url.Parse(target)
		if err != nil || err2 != nil {
			return ""
		}
		return b.ResolveReference(t).String()
	}
	return ""
}

func (c *Client) listModelScope(ctx context.Context, s Spec) (*Listing, error) {
	ep := c.opts.Endpoints.modelscope()
	q := url.Values{"Revision": {s.Rev}, "Recursive": {"true"}}
	if s.Subdir != "" {
		q.Set("Root", s.Subdir)
	}
	var body struct {
		Code    int    `json:"Code"`
		Message string `json:"Message"`
		Data    struct {
			Files []struct {
				Path   string `json:"Path"`
				Size   int64  `json:"Size"`
				Sha256 string `json:"Sha256"`
				Type   string `json:"Type"`
			} `json:"Files"`
		} `json:"Data"`
	}
	if _, err := c.getJSON(ctx, ep+"/api/v1/models/"+s.Repo+"/repo/files?"+q.Encode(), &body); err != nil {
		return nil, err
	}
	if body.Code != 0 && body.Code != http.StatusOK {
		return nil, fmt.Errorf("fetch: modelscope %s: %d %s", s.Repo, body.Code, body.Message)
	}
	l := &Listing{}
	for _, f := range body.Data.Files {
		if f.Type == "tree" {
			continue
		}
		dq := url.Values{"Revision": {s.Rev}, "FilePath": {f.Path}}
		l.Files = append(l.Files, File{
			Path: f.Path, Size: f.Size, SHA256: strings.ToLower(f.Sha256),
			URL: ep + "/api/v1/models/" + s.Repo + "/repo?" + dq.Encode(),
		})
	}
	return l, nil
}

func (c *Client) listHTTP(ctx context.Context, s Spec) (*Listing, error) {
	resp, err := c.do(ctx, http.MethodGet, s.URL, "bytes=0-0")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	size := int64(-1)
	if resp.StatusCode == http.StatusPartialContent {
		if _, total, ok := strings.Cut(resp.Header.Get("Content-Range"), "/"); ok {
			size, _ = strconv.ParseInt(total, 10, 64)
		}
	} else if resp.ContentLength >= 0 {
		size = resp.ContentLength
	}
	if size < 0 {
		return nil, fmt.Errorf("fetch: %s: server did not report a size", redact(s.URL))
	}
	return &Listing{Files: []File{{Path: s.Name(), Size: size, SHA256: s.SHA256, URL: s.URL}}}, nil
}
