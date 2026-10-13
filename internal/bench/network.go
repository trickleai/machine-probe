package bench

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"
)

var latencyHosts = []string{"github.com", "registry.npmjs.org", "example.com", "www.cloudflare.com"}

func client(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		Transport:     &http.Transport{Proxy: http.ProxyFromEnvironment, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func median(xs []float64) float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

// medianOf runs fn tries times and returns the median latency in ms of successful attempts.
func medianOf(ctx context.Context, tries int, fn func() error) (float64, error) {
	var ms []float64
	var last error
	for i := 0; i < tries && ctx.Err() == nil; i++ {
		t := time.Now()
		if err := fn(); err != nil {
			last = err
			continue
		}
		ms = append(ms, float64(time.Since(t).Microseconds())/1000)
	}
	if len(ms) == 0 {
		if last == nil {
			last = ctx.Err()
		}
		return 0, last
	}
	return median(ms), nil
}

func networkTests(p Profile) []Test {
	ts := []Test{{
		ID: "net.latency", Group: "network", Title: "Network · HTTPS latency, DNS and TCP connect", Tool: "go-net/http", Once: true,
		Run: func(ctx context.Context, e *Env) (Rep, error) {
			var r Rep
			var failed []string
			c := client(8 * time.Second)
			for _, h := range latencyHosts {
				e.Progress("https " + h)
				v, err := medianOf(ctx, 5, func() error {
					req, _ := http.NewRequestWithContext(ctx, http.MethodHead, "https://"+h+"/", nil)
					resp, err := c.Do(req)
					if err == nil {
						resp.Body.Close()
					}
					return err
				})
				if err != nil {
					failed = append(failed, h)
					continue
				}
				r.Lo("mp_https_"+h+"_ms", "ms", v)
			}
			var dns []float64
			for _, h := range latencyHosts {
				if v, err := medianOf(ctx, 3, func() error {
					_, err := net.DefaultResolver.LookupHost(ctx, h)
					return err
				}); err == nil {
					dns = append(dns, v)
				}
			}
			if len(dns) > 0 {
				r.Lo("mp_dns_median_ms", "ms", median(dns))
			}
			d := net.Dialer{Timeout: 5 * time.Second}
			if v, err := medianOf(ctx, 5, func() error {
				conn, err := d.DialContext(ctx, "tcp", "1.1.1.1:443")
				if err == nil {
					conn.Close()
				}
				return err
			}); err == nil {
				r.Lo("mp_tcp_connect_ms", "ms", v)
			}
			if len(failed) > 0 {
				r.Notes = "Unreachable: " + strings.Join(failed, ", ")
			}
			if len(r.Measures) == 0 {
				return r, fmt.Errorf("no network endpoint reachable")
			}
			return r, ctx.Err()
		},
	}}
	if p.Download {
		ts = append(ts, Test{
			ID: "net.throughput", Group: "network", Title: "Network · download 50 MB / upload 10 MB (Cloudflare)", Tool: "go-net/http", Once: true,
			Run: func(ctx context.Context, e *Env) (Rep, error) {
				c := client(90 * time.Second)
				var r Rep
				var errs []string
				e.Progress("download")
				t := time.Now()
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://speed.cloudflare.com/__down?bytes=50000000", nil)
				if resp, err := c.Do(req); err != nil {
					errs = append(errs, "download: "+err.Error())
				} else {
					n, err := io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					if err != nil || n == 0 {
						errs = append(errs, fmt.Sprintf("download: %d bytes, %v", n, err))
					} else {
						r.Hi("mp_download_mbps", "Mbit/s", float64(n)*8/1e6/time.Since(t).Seconds())
					}
				}
				e.Progress("upload")
				body := make([]byte, 10_000_000)
				t = time.Now()
				req, _ = http.NewRequestWithContext(ctx, http.MethodPost, "https://speed.cloudflare.com/__up", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/octet-stream")
				if resp, err := c.Do(req); err != nil {
					errs = append(errs, "upload: "+err.Error())
				} else {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					if resp.StatusCode >= 400 {
						errs = append(errs, "upload: HTTP "+resp.Status)
					} else {
						r.Hi("mp_upload_mbps", "Mbit/s", float64(len(body))*8/1e6/time.Since(t).Seconds())
					}
				}
				r.Notes = strings.Join(errs, "; ")
				if len(r.Measures) == 0 {
					return r, fmt.Errorf("%s", r.Notes)
				}
				return r, ctx.Err()
			},
		})
	}
	return ts
}
