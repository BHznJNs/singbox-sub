package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("SINGBOX_SUB_HELPER"); mode != "" {
		if len(os.Args) < 2 {
			os.Exit(2)
		}
		if os.Args[1] == "check" {
			if mode == "check-fail" {
				os.Exit(1)
			}
			os.Exit(0)
		}
		if mode == "start-fail" {
			os.Exit(1)
		}
		b, err := os.ReadFile(os.Args[3])
		if err != nil {
			os.Exit(2)
		}
		var cfg struct {
			Inbounds []struct {
				Port int `json:"listen_port"`
			} `json:"inbounds"`
		}
		if json.Unmarshal(b, &cfg) != nil || len(cfg.Inbounds) != 1 {
			os.Exit(2)
		}
		listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Inbounds[0].Port)))
		if err != nil {
			os.Exit(1)
		}
		if mode == "exit-later" {
			go func() { time.Sleep(1200 * time.Millisecond); os.Exit(1) }()
		}
		for {
			conn, err := listener.Accept()
			if err != nil {
				os.Exit(1)
			}
			_ = conn.Close()
		}
	}
	os.Exit(m.Run())
}

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestSettings(t *testing.T) {
	cfg, err := readSettings(env(map[string]string{"SERVER_ADDR": "203.0.113.1"}))
	if err != nil || cfg.ServerPort != 24443 || cfg.ServerName != defaultRealityServer || cfg.SubBaseURL != "http://localhost:8080" {
		t.Fatalf("unexpected defaults: %+v, %v", cfg, err)
	}
	for _, host := range []string{"203.0.113.1", "2001:db8::1", "proxy.example.com"} {
		if _, err := readSettings(env(map[string]string{"SERVER_ADDR": host, "SERVER_PORT": "34443", "SUB_BASE_URL": "https://sub.example.com/prefix/"})); err != nil {
			t.Errorf("rejected %q: %v", host, err)
		}
	}
	for _, tc := range []struct{ key, value string }{
		{"SERVER_ADDR", ""}, {"SERVER_ADDR", "https://example.com"}, {"SERVER_ADDR", "a:443"},
		{"SERVER_ADDR", "bad\nname"}, {"SERVER_ADDR", "-bad.example"}, {"SERVER_ADDR", "[::1]"},
		{"SERVER_PORT", "0"}, {"SERVER_PORT", "65536"}, {"SERVER_PORT", "8080"},
		{"SERVER_PORT", "+443"}, {"SERVER_PORT", "12.5"}, {"SERVER_PORT", "abc"},
		{"REALITY_SERVER_NAME", "127.0.0.1"}, {"REALITY_SERVER_NAME", "bad/path"},
		{"SUB_BASE_URL", "ftp://example.com"}, {"SUB_BASE_URL", "https://user:pass@example.com"},
		{"SUB_BASE_URL", "https://example.com/?token=x"}, {"SUB_BASE_URL", "https://example.com/#x"},
		{"SUB_BASE_URL", "https://example.com:65536"}, {"SUB_BASE_URL", "https://example.com\n"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			values := map[string]string{"SERVER_ADDR": "example.com"}
			values[tc.key] = tc.value
			if _, err := readSettings(env(values)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestIdentityPersistence(t *testing.T) {
	dir := t.TempDir()
	first, err := loadIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.validate(); err != nil {
		t.Fatal(err)
	}
	second, err := loadIdentity(dir)
	if err != nil || first != second {
		t.Fatalf("identity changed: %v", err)
	}
	other, err := loadIdentity(t.TempDir())
	if err != nil || other.UUID == first.UUID || other.Token == first.Token || other.PrivateKey == first.PrivateKey {
		t.Fatalf("new directory reused identity: %v", err)
	}
}

func TestCorruptIdentity(t *testing.T) {
	for _, corruption := range []string{"invalid-json", "missing-file", "missing-directory", "missing-token", "duplicate-short-id-token", "mismatched-key", "invalid-version", "interrupted"} {
		t.Run(corruption, func(t *testing.T) {
			dir := t.TempDir()
			id, err := loadIdentity(dir)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "identity", "state.json")
			switch corruption {
			case "missing-file", "interrupted":
				err = os.Remove(path)
			case "missing-directory":
				err = os.RemoveAll(filepath.Dir(path))
				if err == nil {
					err = os.WriteFile(filepath.Join(dir, "sing-box.json"), []byte("{}"), 0600)
				}
			case "invalid-json":
				err = os.WriteFile(path, []byte("{"), 0600)
			default:
				switch corruption {
				case "missing-token":
					id.Token = ""
				case "duplicate-short-id-token":
					id.Token = id.ShortID
				case "mismatched-key":
					id.PublicKey = strings.Repeat("A", 43)
				case "invalid-version":
					id.Version = 2
				}
				b, _ := json.Marshal(id)
				err = os.WriteFile(path, b, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			if _, err := loadIdentity(dir); err == nil {
				t.Fatal("corrupt identity accepted")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("corrupt identity was overwritten")
			}
		})
	}
}

func TestConfigConsistency(t *testing.T) {
	id, err := newIdentity()
	if err != nil {
		t.Fatal(err)
	}
	cfg := settings{"2001:db8::1", 34443, defaultRealityServer, "https://sub.example.com"}
	server, err := serverConfig(cfg, id)
	if err != nil || !json.Valid(server) {
		t.Fatalf("invalid JSON: %v", err)
	}
	var parsed map[string]any
	_ = json.Unmarshal(server, &parsed)
	in := parsed["inbounds"].([]any)[0].(map[string]any)
	if in["listen_port"] != float64(cfg.ServerPort) || in["users"].([]any)[0].(map[string]any)["uuid"] != id.UUID {
		t.Fatal("server identity mismatch")
	}
	if !bytes.Contains(server, []byte(id.PrivateKey)) || !bytes.Contains(server, []byte(id.ShortID)) {
		t.Fatal("missing REALITY identity")
	}
	yaml := string(subscription(cfg, id))
	for _, expected := range []string{id.UUID, id.PublicKey, id.ShortID, cfg.ServerAddr, "port: 34443", "flow: xtls-rprx-vision", "MATCH,PROXY", "proxy-groups:", "dns:"} {
		if !strings.Contains(yaml, expected) {
			t.Errorf("subscription missing %q", expected)
		}
	}
	if strings.Contains(yaml, id.PrivateKey) || strings.Contains(yaml, id.Token) {
		t.Fatal("subscription leaked private state")
	}
}

func TestHTTPIsolation(t *testing.T) {
	id, _ := newIdentity()
	content := []byte("proxies: []\n")
	handler := subscriptionHandler(id, content)
	path := "/" + id.Token + "/clash.yaml"
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", path, 200}, {"HEAD", path, 200}, {"POST", path, 405},
		{"GET", "/", 404}, {"GET", "/wrong/clash.yaml", 404},
		{"GET", "/identity/state.json", 404}, {"GET", "/sing-box.json", 404},
		{"GET", path + "/", 404}, {"GET", path + "?x=1", 404},
		{"GET", "/" + id.Token + "/../identity/state.json", 404},
		{"GET", "/" + id.Token + "/%63lash.yaml", 404},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("got %d, want %d", w.Code, tc.status)
			}
			if tc.status == 200 && tc.method == "GET" && !bytes.Equal(w.Body.Bytes(), content) {
				t.Fatal("invalid subscription response")
			}
			if tc.method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD returned a body")
			}
			if tc.status != 200 && bytes.Contains(w.Body.Bytes(), content) {
				t.Fatal("unauthorized subscription response")
			}
		})
	}
}

type lockedBuffer struct {
	sync.Mutex
	b bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.Lock()
	defer b.Unlock()
	return b.b.String()
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitLog(t *testing.T, log *lockedBuffer, text string, done <-chan error) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case err := <-done:
			t.Fatalf("service stopped: %v\n%s", err, log.String())
		case <-deadline:
			t.Fatalf("timed out waiting for %q\n%s", text, log.String())
		case <-time.After(25 * time.Millisecond):
			if strings.Contains(log.String(), text) {
				return
			}
		}
	}
}

func TestLifecycle(t *testing.T) {
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"check-fail", "start-fail", "occupied-http", "occupied-proxy", "exit-later", "normal"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("SINGBOX_SUB_HELPER", mode)
			if mode == "occupied-http" {
				l, err := net.Listen("tcp4", "127.0.0.1:8080")
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var output lockedBuffer
			done := make(chan error, 1)
			dir := t.TempDir()
			port := freePort(t)
			if mode == "occupied-proxy" {
				l, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
			}
			go func() {
				done <- run(ctx, env(map[string]string{"SERVER_ADDR": "127.0.0.1", "SERVER_PORT": strconv.Itoa(port)}), dir, helper, &output)
			}()
			if mode == "normal" || mode == "exit-later" {
				waitLog(t, &output, "Services ready.", done)
				if mode == "normal" {
					id, err := loadIdentity(dir)
					if err != nil {
						t.Fatal(err)
					}
					resp, err := http.Get("http://127.0.0.1:8080/" + id.Token + "/clash.yaml")
					if err != nil {
						t.Fatal(err)
					}
					b, _ := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					if resp.StatusCode != 200 || !bytes.Contains(b, []byte(id.UUID)) {
						t.Fatal("invalid live subscription")
					}
					cancel()
				}
			}
			select {
			case err := <-done:
				if (mode == "normal") != (err == nil) {
					t.Fatalf("unexpected result: %v\n%s", err, output.String())
				}
			case <-time.After(15 * time.Second):
				t.Fatal("service did not exit")
			}
			if mode != "normal" && mode != "exit-later" && strings.Contains(output.String(), "Services ready.") {
				t.Fatal("startup failure logged success")
			}
			if mode == "normal" || mode == "exit-later" {
				conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
				if err == nil {
					conn.Close()
					t.Fatal("child process still listening")
				}
			}
		})
	}
}

func TestRealKernels(t *testing.T) {
	binary := os.Getenv("SING_BOX_TEST_BINARY")
	mihomo := os.Getenv("MIHOMO_TEST_BINARY")
	if binary == "" || mihomo == "" {
		t.Skip("set SING_BOX_TEST_BINARY and MIHOMO_TEST_BINARY for real-kernel integration")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	var output lockedBuffer
	done := make(chan error, 1)
	port := freePort(t)
	values := map[string]string{"SERVER_ADDR": "127.0.0.1", "SERVER_PORT": strconv.Itoa(port)}
	go func() { done <- run(ctx, env(values), dir, binary, &output) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("server: %v\n%s", err, output.String())
			}
		case <-time.After(10 * time.Second):
			t.Error("server shutdown timed out")
		}
	}()
	waitLog(t, &output, "Services ready.", done)
	id, err := loadIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(dir, "clash.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	clientDir := t.TempDir()
	originalPath := filepath.Join(clientDir, "original.yaml")
	if err := os.WriteFile(originalPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(mihomo, "-t", "-d", clientDir, "-f", originalPath)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Mihomo rejected original subscription: %v\n%s", err, b)
	}
	proxyPort := freePort(t)
	config := strings.Replace(string(original), "mixed-port: 7890", fmt.Sprintf("mixed-port: %d", proxyPort), 1)
	// Route the local test destination through VLESS, not the production private-network bypass.
	config = config[:strings.Index(config, "rules:\n")] + "rules:\n  - MATCH,PROXY\n"
	configPath := filepath.Join(clientDir, "test.yaml")
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command(mihomo, "-d", clientDir, "-f", configPath)
	var clientOutput lockedBuffer
	cmd.Stdout, cmd.Stderr = &clientOutput, &clientOutput
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	clientExited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(clientExited) }()
	defer stopProcess(cmd, clientExited)
	if err := waitForProxy(ctx, proxyPort, clientExited); err != nil {
		t.Fatalf("Mihomo not ready: %v\n%s", err, clientOutput.String())
	}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "through-reality:"+id.UUID) }))
	defer target.Close()
	proxyURL := "http://127.0.0.1:" + strconv.Itoa(proxyPort)
	req, _ := http.NewRequest(http.MethodGet, target.URL, nil)
	proxyReq, _ := http.NewRequest(http.MethodGet, proxyURL, nil)
	transport := &http.Transport{Proxy: http.ProxyURL(proxyReq.URL)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("REALITY proxy request failed: %v\nserver: %s\nMihomo: %s", err, output.String(), clientOutput.String())
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !reflect.DeepEqual(string(b), "through-reality:"+id.UUID) {
		t.Fatalf("unexpected proxied body: %q\n%s\n%s", b, output.String(), clientOutput.String())
	}
	t.Log("Mihomo accepted the original YAML and completed an HTTP request through VLESS/TCP/REALITY/Vision")
}

func TestHTTPServiceFailure(t *testing.T) {
	t.Setenv("SINGBOX_SUB_HELPER", "normal")
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cfg := settings{"127.0.0.1", freePort(t), defaultRealityServer, "http://localhost:8080"}
	dir := t.TempDir()
	id, err := loadIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	config, _ := serverConfig(cfg, id)
	configPath := filepath.Join(dir, "sing-box.json")
	if err := atomicWrite(configPath, config); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "0.0.0.0:8080")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var output lockedBuffer
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, id, subscription(cfg, id), configPath, helper, listener, &output) }()
	waitLog(t, &output, "Services ready.", done)
	_ = listener.Close()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "subscription service exited unexpectedly") {
			t.Fatalf("HTTP failure not propagated: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("HTTP failure did not stop supervisor")
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.ServerPort)), 100*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatal("proxy remained running after HTTP failure")
	}
}

func TestRestartPreservesIdentity(t *testing.T) {
	t.Setenv("SINGBOX_SUB_HELPER", "normal")
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	values := map[string]string{"SERVER_ADDR": "example.com", "SERVER_PORT": strconv.Itoa(freePort(t)), "SUB_BASE_URL": "https://sub.example.com"}
	var previous identity
	var previousURL string
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		var output lockedBuffer
		done := make(chan error, 1)
		go func() { done <- run(ctx, env(values), dir, helper, &output) }()
		waitLog(t, &output, "Services ready.", done)
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		id, err := loadIdentity(dir)
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && (id != previous || output.String() != previousURL) {
			t.Fatal("identity or subscription URL changed after restart")
		}
		previous, previousURL = id, output.String()
	}
}
