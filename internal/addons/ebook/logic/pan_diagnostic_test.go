package album

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type diagnosticTransport struct {
	base http.RoundTripper
	t    *testing.T
}

func (d diagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	start := time.Now()
	res, err := d.base.RoundTrip(r)
	if err != nil {
		return res, err
	}
	if r.URL.Host == "down.szputy.com" {
		fields := strings.Split(r.URL.Query().Get("auth_key"), "-")
		left := int64(0)
		if len(fields) == 4 {
			stamp, _ := strconv.ParseInt(fields[0], 10, 64)
			left = stamp - time.Now().Unix()
		}
		d.t.Logf("CDN status=%d signed=%t remaining=%ds elapsed=%s", res.StatusCode, len(fields) == 4, left, time.Since(start).Round(time.Millisecond))
	}

	if r.URL.Path == "/api/v1/direct-link/url" {
		b, e := io.ReadAll(io.LimitReader(res.Body, 2<<20))
		res.Body.Close()
		if e != nil {
			return nil, e
		}
		res.Body = io.NopCloser(bytes.NewReader(b))
		var v struct {
			Data map[string]interface{} `json:"data"`
		}
		json.Unmarshal(b, &v)
		for k, val := range v.Data {
			u, e := url.Parse(toDiagnosticString(val))
			if e == nil && u.Host != "" {
				d.t.Logf("direct field=%s scheme=%s host=%s", k, u.Scheme, u.Host)
			} else {
				d.t.Logf("direct field=%s type=%T", k, val)
			}
		}
	}
	return res, nil
}
func toDiagnosticString(v interface{}) string { s, _ := v.(string); return s }
func TestConfiguredPanDiagnostic(t *testing.T) {
	root := os.Getenv("ALBUM_DIAGNOSTIC_ROOT")
	if root == "" {
		t.Skip("explicit local diagnostic only")
	}
	root, _ = filepath.Abs(root)
	t.Chdir(root)
	cfg, err := readStorageConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Profiles) == 0 {
		t.Fatal("no profiles")
	}
	old := panHTTP.Transport
	panHTTP.Transport = diagnosticTransport{old, t}
	defer func() { panHTTP.Transport = old }()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err = CheckStorage(ctx, cfg.Profiles[len(cfg.Profiles)-1].ID); err != nil {
		t.Fatal(err)
	}
}
