package yqksign

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ClientOptions supplies the packaged bootstrap URLs and the APP's complete
// common request fields. No device ID, account token or user ID is built in.
type ClientOptions struct {
	BootstrapConfigURLs []string
	Common              map[string]any
	HTTPClient          *http.Client
	// MaxAttempts bounds ALL HTTP requests in one Discover or Call, including
	// bootstrap GETs, initialization POSTs and backend retries. Default: 16.
	MaxAttempts int
	// MaxResponseBytes limits each downloaded JSON document. Default: 2 MiB.
	MaxResponseBytes int64
}

// ClientState can be JSON encoded and persisted by the caller. It contains no
// common request fields or account credentials. Bootstrap URLs remain available
// even when runtime configuration changes or an imported state becomes stale.
type ClientState struct {
	ConfigURLs      []string  `json:"configURLs"`
	Backends        []string  `json:"backends"`
	ActiveBackend   string    `json:"activeBackend"`
	LastDiscoveryAt time.Time `json:"lastDiscoveryAt"`
}

// Client discovers dynamic HTTPS backends and signs read-only APP requests.
// Calls are serialized to keep discovery and failover state consistent. Use
// separate clients for different devices/accounts or parallel worker pools.
type Client struct {
	mu        sync.Mutex
	http      *http.Client
	bootstrap []string
	common    map[string]any
	max       int
	limit     int64
	state     ClientState
}

// Response preserves data as JSON so callers can decode large integer IDs with
// DecodeData without converting them to float64.
type Response struct {
	Result      bool            `json:"result"`
	Msg         string          `json:"msg"`
	Data        json.RawMessage `json:"data"`
	OperateCode json.RawMessage `json:"operateCode"`
	Backend     string          `json:"-"`
}

func (r *Response) DecodeData(target any) error {
	decoder := json.NewDecoder(bytes.NewReader(r.Data))
	decoder.UseNumber()
	return decoder.Decode(target)
}

// BusinessError is an authenticated application-level failure. In particular,
// a sign error is returned immediately and never causes backend cycling.
type BusinessError struct{ Response *Response }

func (e *BusinessError) Error() string { return "Together API: " + e.Response.Msg }

type HTTPStatusError struct {
	URL        string
	StatusCode int
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("%s: HTTP %d", e.URL, e.StatusCode)
}

// DiscoveryError records bounded failed attempts without request bodies/tokens.
type DiscoveryError struct{ Attempts []string }

func (e *DiscoveryError) Error() string {
	return "no usable Together backend within request budget: " + strings.Join(e.Attempts, "; ")
}

func NewClient(options ClientOptions) (*Client, error) {
	bootstrap, err := validatedURLs(options.BootstrapConfigURLs, false)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	if len(bootstrap) == 0 {
		return nil, errors.New("at least one HTTPS bootstrap config URL is required")
	}
	if len(options.Common) == 0 {
		return nil, errors.New("complete APP common request parameters are required")
	}
	if _, err := GenerateSign(options.Common); err != nil {
		return nil, fmt.Errorf("common parameters: %w", err)
	}
	max := options.MaxAttempts
	if max == 0 {
		max = 16
	}
	if max < 1 || max > 64 {
		return nil, errors.New("MaxAttempts must be between 1 and 64")
	}
	limit := options.MaxResponseBytes
	if limit == 0 {
		limit = 2 * 1024 * 1024
	}
	if limit < 1 {
		return nil, errors.New("MaxResponseBytes must be positive")
	}
	var hc http.Client
	if options.HTTPClient == nil {
		hc = http.Client{Timeout: 20 * time.Second}
	} else {
		hc = *options.HTTPClient
		if hc.Timeout == 0 {
			hc.Timeout = 20 * time.Second
		}
	}
	// Preserve the caller's transport/certificate pool and redirect policy;
	// never disable certificate verification or allow an HTTPS downgrade.
	previousRedirect := hc.CheckRedirect
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return errors.New("HTTPS downgrade redirect rejected")
		}
		if previousRedirect != nil {
			if err := previousRedirect(req, via); err != nil {
				return err
			}
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		if budget, ok := req.Context().Value(budgetContextKey{}).(*requestBudget); ok {
			if budget.left <= 0 {
				return budget.exhausted()
			}
			budget.left--
		}
		return nil
	}
	common := make(map[string]any, len(options.Common))
	for key, value := range options.Common {
		common[key] = value
	}
	return &Client{http: &hc, bootstrap: bootstrap, common: common, max: max, limit: limit}, nil
}

func cloneState(state ClientState) ClientState {
	state.ConfigURLs = append([]string(nil), state.ConfigURLs...)
	state.Backends = append([]string(nil), state.Backends...)
	return state
}

func (c *Client) Snapshot() ClientState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneState(c.state)
}

// Restore imports caller-managed runtime state. It validates all URLs before
// replacing state; the next Call can fail over and rediscover stale domains.
func (c *Client) Restore(state ClientState) error {
	configs, err := validatedURLs(state.ConfigURLs, false)
	if err != nil {
		return err
	}
	backends, err := validatedURLs(state.Backends, true)
	if err != nil {
		return err
	}
	if state.ActiveBackend != "" {
		active, err := validatedURLs([]string{state.ActiveBackend}, true)
		if err != nil {
			return err
		}
		state.ActiveBackend = active[0]
		backends = orderedUnique(active, backends)
	}
	state.ConfigURLs, state.Backends = configs, backends
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = cloneState(state)
	return nil
}

type requestBudget struct {
	left     int
	failures []string
}

type budgetContextKey struct{}

func (b *requestBudget) failed(err error) { b.failures = append(b.failures, err.Error()) }
func (b *requestBudget) exhausted() error {
	return &DiscoveryError{Attempts: append([]string(nil), b.failures...)}
}

// Discover performs bootstrap -> config backend list -> signed basic/init.
// Previously cached backends are tried first. The init response refreshes the
// runtime config entry list; the successful backend is selected for Call.
func (c *Client) Discover(ctx context.Context) (ClientState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	budget := &requestBudget{left: c.max}
	if err := c.discover(ctx, budget); err != nil {
		return cloneState(c.state), err
	}
	return cloneState(c.state), nil
}

func (c *Client) discover(ctx context.Context, budget *requestBudget) error {
	tried := make(map[string]bool)
	for _, backend := range c.backends() {
		tried[backend] = true
		if err := c.initialize(ctx, backend, budget); err == nil {
			return nil
		} else if terminal(ctx, err) {
			return err
		} else {
			budget.failed(err)
		}
		if budget.left == 0 {
			return budget.exhausted()
		}
	}
	for _, config := range orderedUnique(c.state.ConfigURLs, c.bootstrap) {
		if budget.left == 0 {
			break
		}
		backends, err := c.getConfig(ctx, config, budget)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			budget.failed(err)
			continue
		}
		c.state.Backends = orderedUnique(backends, c.state.Backends)
		for _, backend := range backends {
			if tried[backend] || budget.left == 0 {
				continue
			}
			tried[backend] = true
			if err := c.initialize(ctx, backend, budget); err == nil {
				return nil
			} else if terminal(ctx, err) {
				return err
			} else {
				budget.failed(err)
			}
		}
	}
	return budget.exhausted()
}

func (c *Client) backends() []string {
	if c.state.ActiveBackend == "" {
		return append([]string(nil), c.state.Backends...)
	}
	return orderedUnique([]string{c.state.ActiveBackend}, c.state.Backends)
}

func (c *Client) initialize(ctx context.Context, backend string, budget *requestBudget) error {
	response, err := c.post(ctx, backend, "/v2/api/basic/init", nil, budget)
	if err != nil {
		return err
	}
	var data struct {
		ConfigURLs []string `json:"apiConfigDomains"`
	}
	if err := response.DecodeData(&data); err != nil {
		return fmt.Errorf("%s: invalid initialization data: %w", backend, err)
	}
	configs, err := validatedURLs(data.ConfigURLs, false)
	if err != nil {
		return fmt.Errorf("%s: invalid runtime config: %w", backend, err)
	}
	if len(configs) == 0 {
		return fmt.Errorf("%s: initialization has no apiConfigDomains", backend)
	}
	c.state.ConfigURLs = configs
	c.state.ActiveBackend = backend
	c.state.Backends = orderedUnique([]string{backend}, c.state.Backends)
	c.state.LastDiscoveryAt = time.Now().UTC()
	return nil
}

// Call sends a signed POST with common + business fields and a fresh requestId.
// It fails over on connection/TLS errors, malformed JSON, HTTP 408 or HTTP 5xx.
// Application failures and other HTTP 4xx (including 429) return immediately.
// Automatic retry is intended for read-only search/detail/play-URL requests;
// do not use it for account changes or other non-idempotent operations.
func (c *Client) Call(ctx context.Context, path string, business map[string]any) (*Response, error) {
	if !strings.HasPrefix(path, "/v1/api/") && !strings.HasPrefix(path, "/v2/api/") {
		return nil, errors.New("path must start with /v1/api/ or /v2/api/")
	}
	parsed, err := url.Parse(path)
	if err != nil || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" || strings.Contains(path, "..") {
		return nil, errors.New("API path must not contain an authority, query, fragment or traversal")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	budget := &requestBudget{left: c.max}
	if len(c.backends()) == 0 {
		if err := c.discover(ctx, budget); err != nil {
			return nil, err
		}
	}
	tried := make(map[string]bool)
	// At most one rediscovery follows stale backend failures. The same request
	// budget bounds both discovery and API calls, preventing nested retry loops.
	for round := 0; round < 2; round++ {
		for _, backend := range c.backends() {
			if tried[backend] || budget.left == 0 {
				continue
			}
			tried[backend] = true
			response, err := c.post(ctx, backend, path, business, budget)
			if err == nil {
				c.state.ActiveBackend = backend
				return response, nil
			}
			if terminal(ctx, err) {
				return response, err
			}
			budget.failed(err)
		}
		if round == 1 || budget.left == 0 {
			break
		}
		// Clear exhausted backend hints so fresh runtime config entries are read.
		c.state.ActiveBackend = ""
		c.state.Backends = nil
		if err := c.discover(ctx, budget); err != nil {
			return nil, err
		}
		// Initialization just succeeded, so a formerly failing backend may have
		// recovered. Allow one business retry after this rediscovery; the second
		// round and the shared request budget still cap the total attempts.
		tried = make(map[string]bool)
	}
	return nil, budget.exhausted()
}

type parameterError struct{ err error }

func (e *parameterError) Error() string { return "request parameters: " + e.err.Error() }

func terminal(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	var business *BusinessError
	var parameter *parameterError
	var status *HTTPStatusError
	if errors.As(err, &business) || errors.As(err, &parameter) {
		return true
	}
	return errors.As(err, &status) && status.StatusCode != http.StatusRequestTimeout && status.StatusCode < 500
}

func (c *Client) post(ctx context.Context, backend, path string, business map[string]any, budget *requestBudget) (*Response, error) {
	parameters := make(map[string]any, len(c.common)+len(business)+1)
	for key, value := range c.common {
		parameters[key] = value
	}
	for key, value := range business {
		parameters[key] = value
	}
	var randomID [16]byte
	if _, err := rand.Read(randomID[:]); err != nil {
		return nil, &parameterError{err}
	}
	parameters["requestId"] = hex.EncodeToString(randomID[:])
	signed, err := SignRequest(parameters)
	if err != nil {
		return nil, &parameterError{err}
	}
	encoded, err := json.Marshal(signed)
	if err != nil {
		return nil, &parameterError{err}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, backend+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	raw, err := c.request(request, budget)
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Result      *bool           `json:"result"`
		Msg         string          `json:"msg"`
		Data        json.RawMessage `json:"data"`
		OperateCode json.RawMessage `json:"operateCode"`
	}
	if err := decodeJSON(raw, &decoded); err != nil || decoded.Result == nil {
		return nil, fmt.Errorf("%s%s: invalid API JSON response", backend, path)
	}
	response := &Response{Result: *decoded.Result, Msg: decoded.Msg, Data: decoded.Data, OperateCode: decoded.OperateCode, Backend: backend}
	if !response.Result {
		return response, &BusinessError{Response: response}
	}
	return response, nil
}

func (c *Client) getConfig(ctx context.Context, address string, budget *requestBudget) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Cache-Control", "no-cache")
	raw, err := c.request(request, budget)
	if err != nil {
		return nil, err
	}
	var backends []string
	if err := decodeJSON(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}), &backends); err != nil {
		return nil, fmt.Errorf("%s: expected JSON backend URL array: %w", address, err)
	}
	backends, err = validatedURLs(backends, true)
	if err != nil || len(backends) == 0 {
		return nil, fmt.Errorf("%s: invalid or empty backend list", address)
	}
	return backends, nil
}

func (c *Client) request(request *http.Request, budget *requestBudget) ([]byte, error) {
	if err := request.Context().Err(); err != nil {
		return nil, err
	}
	if budget.left <= 0 {
		return nil, budget.exhausted()
	}
	budget.left--
	request = request.WithContext(context.WithValue(request.Context(), budgetContextKey{}, budget))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Dart/3.1 (dart:io)")
	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &HTTPStatusError{URL: request.URL.String(), StatusCode: response.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, c.limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > c.limit {
		return nil, fmt.Errorf("%s: response exceeds %d bytes", request.URL, c.limit)
	}
	return raw, nil
}

func decodeJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("unexpected data after JSON value")
	}
	return nil
}

func validatedURLs(values []string, backend bool) ([]string, error) {
	if len(values) > 256 {
		return nil, errors.New("URL list exceeds 256 entries")
	}
	clean := make([]string, 0, len(values))
	for _, value := range values {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
			return nil, fmt.Errorf("invalid HTTPS URL %q", value)
		}
		if backend && (parsed.RawQuery != "" || (parsed.Path != "" && parsed.Path != "/")) {
			return nil, fmt.Errorf("backend must be an HTTPS origin: %q", value)
		}
		if backend {
			value = "https://" + parsed.Host
		}
		clean = append(clean, value)
	}
	return orderedUnique(clean), nil
}

func orderedUnique(groups ...[]string) []string {
	var result []string
	seen := make(map[string]bool)
	for _, values := range groups {
		for _, value := range values {
			if !seen[value] {
				seen[value] = true
				result = append(result, value)
			}
		}
	}
	return result
}

func (c *Client) Search(ctx context.Context, keyword, nextVal string) (*Response, error) {
	return c.Call(ctx, "/v1/api/search/search", map[string]any{"keyword": keyword, "nextVal": nextVal})
}

func (c *Client) Detail(ctx context.Context, vodID int64) (*Response, error) {
	return c.Call(ctx, "/v2/api/vodInfo/index", map[string]any{"vodId": vodID})
}

func (c *Client) EpisodeDetail(ctx context.Context, episodeID int64) (*Response, error) {
	return c.Call(ctx, "/v2/api/vodInfo/epDetail", map[string]any{"vodEpId": episodeID})
}

// PlayURL retrieves a fresh playback response on demand. Expiring signed URLs
// must not be treated as permanent source URLs or cached indefinitely.
func (c *Client) PlayURL(ctx context.Context, episodeID int64, resolution int) (*Response, error) {
	return c.Call(ctx, "/v2/api/vodInfo/playUrl", map[string]any{"epId": episodeID, "vodResolution": resolution})
}
