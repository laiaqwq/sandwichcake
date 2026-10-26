package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Open streams f from off to its end. Transient failures are retried by
// re-requesting the remaining range; large remainders are fetched with up
// to Options.Connections parallel ranged requests and reassembled in
// order (memory bound: Connections × SegmentSize).
func (c *Client) Open(ctx context.Context, f File, off int64) (io.ReadCloser, error) {
	if off < 0 || off > f.Size {
		return nil, fmt.Errorf("fetch: offset %d out of range for %s (%d bytes)", off, f.Path, f.Size)
	}
	if c.opts.Connections > 1 && f.Size-off > c.opts.SegmentSize {
		return c.openParallel(ctx, f, off), nil
	}
	return &rangeReader{c: c, ctx: ctx, f: f, off: off, end: f.Size}, nil
}

// rangeReader reads [off, end) of f, reconnecting at the current offset
// after errors.
type rangeReader struct {
	c        *Client
	ctx      context.Context
	f        File
	off, end int64
	body     io.ReadCloser
	failures int
}

func (r *rangeReader) connect() error {
	rng := ""
	if r.off > 0 || r.end < r.f.Size {
		rng = "bytes=" + strconv.FormatInt(r.off, 10) + "-" + strconv.FormatInt(r.end-1, 10)
	}
	resp, err := r.c.do(r.ctx, http.MethodGet, r.f.URL, rng)
	if err != nil {
		return err
	}
	if rng != "" && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return fmt.Errorf("fetch: %s: server ignored Range (status %d); resume and parallel download need range support", redact(r.f.URL), resp.StatusCode)
	}
	if rng != "" {
		cr := resp.Header.Get("Content-Range")
		if want := "bytes " + strconv.FormatInt(r.off, 10) + "-"; !strings.HasPrefix(cr, want) {
			resp.Body.Close()
			return fmt.Errorf("fetch: %s: unexpected Content-Range %q", redact(r.f.URL), cr)
		}
	} else if resp.ContentLength >= 0 && resp.ContentLength != r.f.Size {
		resp.Body.Close()
		return fmt.Errorf("fetch: %s: size %d, listing said %d", r.f.Path, resp.ContentLength, r.f.Size)
	}
	r.body = resp.Body
	return nil
}

func (r *rangeReader) Read(p []byte) (int, error) {
	for {
		if r.off >= r.end {
			r.Close()
			return 0, io.EOF
		}
		if r.body == nil {
			if err := r.connect(); err != nil {
				return 0, err
			}
		}
		if max := r.end - r.off; int64(len(p)) > max {
			p = p[:max]
		}
		n, err := r.body.Read(p)
		r.off += int64(n)
		if n > 0 {
			r.failures = 0
		}
		if err == nil || r.off >= r.end {
			return n, nil
		}
		// Truncated or broken body: reconnect at r.off.
		r.Close()
		if n > 0 {
			return n, nil
		}
		if r.ctx.Err() != nil {
			return 0, r.ctx.Err()
		}
		r.failures++
		if r.failures > r.c.opts.Retries {
			return 0, fmt.Errorf("fetch: reading %s at %d: %w", r.f.Path, r.off, err)
		}
		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		case <-time.After(r.c.opts.Backoff(r.failures)):
		}
	}
}

func (r *rangeReader) Close() error {
	if r.body != nil {
		r.body.Close()
		r.body = nil
	}
	return nil
}

type segResult struct {
	data []byte
	err  error
}

// parallelReader fetches fixed-size segments concurrently and yields them
// in order. At most Connections segments are in flight or buffered.
type parallelReader struct {
	cancel context.CancelFunc
	slots  []chan segResult
	sem    chan struct{}
	cur    int
	buf    []byte
	err    error
	wg     sync.WaitGroup
}

func (c *Client) openParallel(ctx context.Context, f File, off int64) *parallelReader {
	ctx, cancel := context.WithCancel(ctx)
	seg := c.opts.SegmentSize
	n := int((f.Size - off + seg - 1) / seg)
	p := &parallelReader{cancel: cancel, slots: make([]chan segResult, n), sem: make(chan struct{}, c.opts.Connections)}
	for i := range p.slots {
		p.slots[i] = make(chan segResult, 1)
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for i := 0; i < n; i++ {
			select {
			case p.sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			start := off + int64(i)*seg
			end := start + seg
			if end > f.Size {
				end = f.Size
			}
			p.wg.Add(1)
			go func(i int, start, end int64) {
				defer p.wg.Done()
				rr := &rangeReader{c: c, ctx: ctx, f: f, off: start, end: end}
				data := make([]byte, end-start)
				_, err := io.ReadFull(rr, data)
				rr.Close()
				p.slots[i] <- segResult{data: data, err: err}
			}(i, start, end)
		}
	}()
	return p
}

func (p *parallelReader) Read(b []byte) (int, error) {
	for len(p.buf) == 0 {
		if p.err != nil {
			return 0, p.err
		}
		if p.cur >= len(p.slots) {
			return 0, io.EOF
		}
		res := <-p.slots[p.cur]
		p.cur++
		<-p.sem
		if res.err != nil {
			p.err = res.err
			return 0, res.err
		}
		p.buf = res.data
	}
	n := copy(b, p.buf)
	p.buf = p.buf[n:]
	return n, nil
}

func (p *parallelReader) Close() error {
	// Workers deliver into buffered slots and the producer honors ctx, so
	// nothing blocks after cancel.
	p.cancel()
	p.wg.Wait()
	return nil
}
