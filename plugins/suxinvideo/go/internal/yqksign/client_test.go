package yqksign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func commonForClientTest() map[string]any {
	return map[string]any{
		"appId": AppID, "bundlerId": "com.novorixalabs.ts", "cus1tom": "cus3tom",
		"deviceInfo": "SHG07", "osInfo": "9", "otherParam": "1", "patchNumber": 0,
		"source": "1001_default", "udid": "test-device", "version": "1.3.64", "versionCode": 1107,
	}
}

func checkSignedRequest(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		t.Errorf("unexpected request method/headers: %s %v", r.Method, r.Header)
	}
	var parameters map[string]any
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&parameters); err != nil {
		t.Errorf("invalid request JSON: %v", err)
		return parameters
	}
	expected, err := GenerateSign(parameters)
	if err != nil || parameters["sign"] != expected {
		t.Errorf("signature does not match transmitted parameters: %v", err)
	}
	if requestID, ok := parameters["requestId"].(string); !ok || len(requestID) != 32 {
		t.Errorf("missing fresh requestId: %v", parameters["requestId"])
	}
	return parameters
}

func TestClientFreshBootstrapFallbackAndInitUpdates(t *testing.T) {
	var badInit, goodInit, detailCalls atomic.Int32
	bad := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badInit.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()
	var good *httptest.Server
	good = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/config.json":
			json.NewEncoder(w).Encode([]string{bad.URL, good.URL})
		case "/v2/api/basic/init":
			checkSignedRequest(t, r)
			n := goodInit.Add(1)
			fmt.Fprintf(w, `{"result":true,"data":{"apiConfigDomains":[%q]}}`, fmt.Sprintf("%s/runtime-%d.json", good.URL, n))
		case "/v2/api/vodInfo/index":
			parameters := checkSignedRequest(t, r)
			if parameters["vodId"] != json.Number("129431") {
				t.Errorf("wrong vodId: %v", parameters["vodId"])
			}
			detailCalls.Add(1)
			fmt.Fprint(w, `{"result":true,"msg":"ok","data":{"vodId":129431,"largeID":9007199254740993,"playerList":[]}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer good.Close()
	common := commonForClientTest()
	before := make(map[string]any)
	for k, v := range common {
		before[k] = v
	}
	client, err := NewClient(ClientOptions{BootstrapConfigURLs: []string{good.URL + "/config.json"}, Common: common, HTTPClient: good.Client()})
	if err != nil {
		t.Fatal(err)
	}
	state, err := client.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if badInit.Load() != 1 || goodInit.Load() != 1 || state.ActiveBackend != good.URL || !reflect.DeepEqual(state.ConfigURLs, []string{good.URL + "/runtime-1.json"}) {
		t.Fatalf("bootstrap/fallback/init state incorrect: %+v, bad=%d good=%d", state, badInit.Load(), goodInit.Load())
	}
	response, err := client.Detail(context.Background(), 129431)
	if err != nil || detailCalls.Load() != 1 {
		t.Fatalf("detail request: response=%+v err=%v calls=%d", response, err, detailCalls.Load())
	}
	var detail map[string]any
	if err := response.DecodeData(&detail); err != nil || detail["largeID"] != json.Number("9007199254740993") {
		t.Fatalf("large integer precision lost: %v err=%v", detail, err)
	}
	state, err = client.Discover(context.Background())
	if err != nil || !reflect.DeepEqual(state.ConfigURLs, []string{good.URL + "/runtime-2.json"}) || goodInit.Load() != 2 {
		t.Fatalf("runtime config was not refreshed: %+v err=%v", state, err)
	}
	if !reflect.DeepEqual(common, before) {
		t.Fatal("caller common parameters were mutated")
	}
	state.ConfigURLs[0] = "corrupted"
	if client.Snapshot().ConfigURLs[0] == "corrupted" {
		t.Fatal("Snapshot aliases internal state")
	}
}

func TestClientBusinessErrorsNeverCycleBackends(t *testing.T) {
	for _, message := range []string{"sign error", "login required", "film not found"} {
		t.Run(message, func(t *testing.T) {
			var firstCalls, secondCalls atomic.Int32
			second := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				secondCalls.Add(1)
				fmt.Fprint(w, `{"result":true,"data":{}}`)
			}))
			defer second.Close()
			first := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				firstCalls.Add(1)
				checkSignedRequest(t, r)
				json.NewEncoder(w).Encode(map[string]any{"result": false, "msg": message, "data": nil})
			}))
			defer first.Close()
			client, err := NewClient(ClientOptions{BootstrapConfigURLs: []string{first.URL + "/config.json"}, Common: commonForClientTest(), HTTPClient: first.Client()})
			if err != nil {
				t.Fatal(err)
			}
			if err := client.Restore(ClientState{ActiveBackend: first.URL, Backends: []string{first.URL, second.URL}}); err != nil {
				t.Fatal(err)
			}
			response, err := client.Search(context.Background(), "仙逆", "")
			var business *BusinessError
			if !errors.As(err, &business) || response == nil || response.Msg != message || firstCalls.Load() != 1 || secondCalls.Load() != 0 {
				t.Fatalf("business error caused retries: response=%+v err=%v calls=%d/%d", response, err, firstCalls.Load(), secondCalls.Load())
			}
		})
	}
}

func TestClientCallBackendFailureReadsRuntimeConfig(t *testing.T) {
	var badCalls, configCalls, initCalls, searchCalls atomic.Int32
	bad := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badCalls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer bad.Close()
	var current *httptest.Server
	current = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/runtime.json":
			configCalls.Add(1)
			json.NewEncoder(w).Encode([]string{current.URL})
		case "/v2/api/basic/init":
			initCalls.Add(1)
			checkSignedRequest(t, r)
			fmt.Fprintf(w, `{"result":true,"data":{"apiConfigDomains":[%q]}}`, current.URL+"/next.json")
		case "/v1/api/search/search":
			searchCalls.Add(1)
			parameters := checkSignedRequest(t, r)
			if parameters["keyword"] != "仙逆" || parameters["nextVal"] != "" {
				t.Errorf("search parameters changed: %v", parameters)
			}
			fmt.Fprint(w, `{"result":true,"data":{"vodList":[]}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer current.Close()
	client, err := NewClient(ClientOptions{BootstrapConfigURLs: []string{current.URL + "/bootstrap.json"}, Common: commonForClientTest(), HTTPClient: current.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Restore(ClientState{ConfigURLs: []string{current.URL + "/runtime.json"}, ActiveBackend: bad.URL, Backends: []string{bad.URL}}); err != nil {
		t.Fatal(err)
	}
	response, err := client.Search(context.Background(), "仙逆", "")
	if err != nil || response.Backend != current.URL || badCalls.Load() != 1 || configCalls.Load() != 1 || initCalls.Load() != 1 || searchCalls.Load() != 1 {
		t.Fatalf("stale backend rediscovery failed: response=%+v err=%v counts=%d/%d/%d/%d", response, err, badCalls.Load(), configCalls.Load(), initCalls.Load(), searchCalls.Load())
	}
	if !reflect.DeepEqual(client.Snapshot().ConfigURLs, []string{current.URL + "/next.json"}) {
		t.Fatal("new runtime config not cached")
	}
}

func TestClientRequestBudgetAndTLSVerification(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{
		BootstrapConfigURLs: []string{server.URL + "/one.json", server.URL + "/two.json", server.URL + "/three.json"},
		Common:              commonForClientTest(), HTTPClient: server.Client(), MaxAttempts: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Discover(context.Background())
	var discovery *DiscoveryError
	if !errors.As(err, &discovery) || requests.Load() != 2 {
		t.Fatalf("request budget exceeded or wrong error: requests=%d err=%v", requests.Load(), err)
	}
	// A default client must reject the self-signed test certificate. The
	// previous tests pass an explicit trusted pool from httptest.Server.Client.
	client, err = NewClient(ClientOptions{BootstrapConfigURLs: []string{server.URL + "/one.json"}, Common: commonForClientTest(), MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Discover(context.Background())
	if err == nil || requests.Load() != 2 || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("default client disabled TLS verification: requests=%d err=%v", requests.Load(), err)
	}
}

func TestClientRejectsHTTPURLsAndUnsignedStructuredParameters(t *testing.T) {
	if _, err := NewClient(ClientOptions{BootstrapConfigURLs: []string{"http://example.test/config.json"}, Common: commonForClientTest()}); err == nil {
		t.Fatal("plaintext bootstrap was accepted")
	}
	client, err := NewClient(ClientOptions{BootstrapConfigURLs: []string{"https://example.test/config.json"}, Common: commonForClientTest()})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Restore(ClientState{Backends: []string{"https://example.test/path"}}); err == nil {
		t.Fatal("backend with path was accepted")
	}
	if err := client.Restore(ClientState{ActiveBackend: "https://example.test"}); err != nil {
		t.Fatal(err)
	}
	_, err = client.Call(context.Background(), "/v1/api/search/queryNow", map[string]any{"queryValueJson": []any{1}})
	var parameter *parameterError
	if !errors.As(err, &parameter) {
		t.Fatalf("structured signing value was not rejected before network: %v", err)
	}
}

func TestClientRedirectsConsumeTheSameRequestBudget(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		http.Redirect(w, r, fmt.Sprintf("/redirect-%d.json", n), http.StatusFound)
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{
		BootstrapConfigURLs: []string{server.URL + "/config.json"}, Common: commonForClientTest(),
		HTTPClient: server.Client(), MaxAttempts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Discover(context.Background())
	var discovery *DiscoveryError
	if !errors.As(err, &discovery) || requests.Load() != 3 {
		t.Fatalf("redirects escaped request budget: requests=%d err=%v", requests.Load(), err)
	}
}

func TestClientRecoveredBackendIsRetriedAfterSuccessfulInitialization(t *testing.T) {
	var apiCalls, configCalls, initCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/config.json":
			configCalls.Add(1)
			json.NewEncoder(w).Encode([]string{server.URL})
		case "/v2/api/basic/init":
			initCalls.Add(1)
			checkSignedRequest(t, r)
			fmt.Fprintf(w, `{"result":true,"data":{"apiConfigDomains":[%q]}}`, server.URL+"/config.json")
		case "/v2/api/vodInfo/index":
			checkSignedRequest(t, r)
			if apiCalls.Add(1) == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			fmt.Fprint(w, `{"result":true,"data":{"vodId":129431}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{
		BootstrapConfigURLs: []string{server.URL + "/config.json"}, Common: commonForClientTest(),
		HTTPClient: server.Client(), MaxAttempts: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Restore(ClientState{ActiveBackend: server.URL}); err != nil {
		t.Fatal(err)
	}
	response, err := client.Detail(context.Background(), 129431)
	if err != nil || response == nil || apiCalls.Load() != 2 || configCalls.Load() != 1 || initCalls.Load() != 1 {
		t.Fatalf("recovered backend not retried within budget: response=%+v err=%v calls=%d/%d/%d", response, err, apiCalls.Load(), configCalls.Load(), initCalls.Load())
	}
}

func TestClientPlayURLIsFetchedAndSignedForEveryRequest(t *testing.T) {
	var requestIDs []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/api/vodInfo/playUrl" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		parameters := checkSignedRequest(t, r)
		if parameters["epId"] != json.Number("27714327") || parameters["vodResolution"] != json.Number("1") {
			t.Errorf("wrong playback parameters: %v", parameters)
		}
		requestIDs = append(requestIDs, parameters["requestId"].(string))
		fmt.Fprintf(w, `{"result":true,"data":{"playUrl":%q}}`, fmt.Sprintf("https://media.example.test/video.m3u8?generation=%d", len(requestIDs)))
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{BootstrapConfigURLs: []string{server.URL + "/config.json"}, Common: commonForClientTest(), HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Restore(ClientState{ActiveBackend: server.URL}); err != nil {
		t.Fatal(err)
	}
	var previous string
	for i := 0; i < 2; i++ {
		response, err := client.PlayURL(context.Background(), 27714327, 1)
		if err != nil {
			t.Fatal(err)
		}
		var data struct {
			PlayURL string `json:"playUrl"`
		}
		if err := response.DecodeData(&data); err != nil || data.PlayURL == "" || data.PlayURL == previous {
			t.Fatalf("playback URL was reused instead of fetched: %+v err=%v", data, err)
		}
		previous = data.PlayURL
	}
	if len(requestIDs) != 2 || requestIDs[0] == requestIDs[1] {
		t.Fatalf("request IDs were reused: %v", requestIDs)
	}
}

func TestClientStateRoundTripAndInvalidRestoreAreIndependent(t *testing.T) {
	options := ClientOptions{BootstrapConfigURLs: []string{"https://bootstrap.example.test/config.json"}, Common: commonForClientTest()}
	client, err := NewClient(options)
	if err != nil {
		t.Fatal(err)
	}
	original := ClientState{
		ConfigURLs: []string{"https://runtime.example.test/cf.json"}, Backends: []string{"https://api.example.test"},
		ActiveBackend: "https://api.example.test", LastDiscoveryAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := client.Restore(original); err != nil {
		t.Fatal(err)
	}
	original.ConfigURLs[0] = "https://mutated.example.test/cf.json"
	encoded, err := json.Marshal(client.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "test-device") || strings.Contains(string(encoded), "appId") {
		t.Fatal("state export contains common request fields")
	}
	var persisted ClientState
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewClient(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Restore(persisted); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restarted.Snapshot(), client.Snapshot()) {
		t.Fatal("persisted state changed during restore")
	}
	if err := restarted.Restore(ClientState{ConfigURLs: []string{"http://invalid.example.test/cf.json"}}); err == nil {
		t.Fatal("invalid restored state was accepted")
	}
	if !reflect.DeepEqual(restarted.Snapshot(), client.Snapshot()) {
		t.Fatal("invalid restore replaced working state")
	}
}

func TestClientHTTP429DoesNotCycleBackends(t *testing.T) {
	var firstCalls, secondCalls atomic.Int32
	second := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls.Add(1)
		fmt.Fprint(w, `{"result":true,"data":{}}`)
	}))
	defer second.Close()
	first := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer first.Close()
	client, err := NewClient(ClientOptions{BootstrapConfigURLs: []string{first.URL + "/config.json"}, Common: commonForClientTest(), HTTPClient: first.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Restore(ClientState{ActiveBackend: first.URL, Backends: []string{second.URL}}); err != nil {
		t.Fatal(err)
	}
	_, err = client.Detail(context.Background(), 129431)
	var status *HTTPStatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusTooManyRequests || firstCalls.Load() != 1 || secondCalls.Load() != 0 {
		t.Fatalf("HTTP 429 caused failover: err=%v calls=%d/%d", err, firstCalls.Load(), secondCalls.Load())
	}
}
