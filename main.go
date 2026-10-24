package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func subscriptionHandler(id identity, content []byte) http.Handler {
	path := "/" + id.Token + "/clash.yaml"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if subtle.ConstantTimeCompare([]byte(r.URL.EscapedPath()), []byte(path)) != 1 || r.URL.RawQuery != "" || r.URL.ForceQuery {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="clash.yaml"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		if r.Method == http.MethodGet {
			_, _ = w.Write(content)
		}
	})
}

func stopProcess(cmd *exec.Cmd, done <-chan struct{}) {
	_ = cmd.Process.Signal(os.Interrupt)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

func waitForProxy(ctx context.Context, port int, done <-chan struct{}) error {
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var readySince time.Time
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return errors.New("sing-box exited before becoming ready")
		case <-timer.C:
			return errors.New("sing-box startup timed out")
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
			if err != nil {
				readySince = time.Time{}
				continue
			}
			_ = conn.Close()
			if readySince.IsZero() {
				readySince = time.Now()
			}
			if time.Since(readySince) >= 300*time.Millisecond {
				return nil
			}
		}
	}
}

func run(ctx context.Context, getenv func(string) string, dataDir, binary string, output io.Writer) error {
	cfg, err := readSettings(getenv)
	if err != nil {
		return err
	}
	id, err := loadIdentity(dataDir)
	if err != nil {
		return fmt.Errorf("load identity: %w", err)
	}
	config, err := serverConfig(cfg, id)
	if err != nil {
		return err
	}
	configPath := filepath.Join(dataDir, "sing-box.json")
	if err := atomicWrite(configPath, config); err != nil {
		return fmt.Errorf("write sing-box config: %w", err)
	}
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	check := exec.CommandContext(checkCtx, binary, "check", "-c", configPath)
	check.Stdout, check.Stderr = output, output
	if err := check.Run(); err != nil {
		return fmt.Errorf("sing-box config check failed: %w", err)
	}
	content := subscription(cfg, id)
	if err := atomicWrite(filepath.Join(dataDir, "clash.yaml"), content); err != nil {
		return fmt.Errorf("write subscription: %w", err)
	}
	listener, err := net.Listen("tcp4", "0.0.0.0:8080")
	if err != nil {
		return fmt.Errorf("subscription listener failed: %w", err)
	}
	return serve(ctx, cfg, id, content, configPath, binary, listener, output)
}

func serve(ctx context.Context, cfg settings, id identity, content []byte, configPath, binary string, listener net.Listener, output io.Writer) error {
	defer listener.Close()
	cmd := exec.Command(binary, "run", "-c", configPath)
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start sing-box: %w", err)
	}
	exited := make(chan struct{})
	var processErr error
	go func() {
		processErr = cmd.Wait()
		close(exited)
	}()
	defer stopProcess(cmd, exited)
	if err := waitForProxy(ctx, cfg.ServerPort, exited); err != nil {
		return err
	}
	server := &http.Server{
		Handler: subscriptionHandler(id, content), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192,
	}
	defer server.Close()
	httpDone := make(chan error, 1)
	go func() { httpDone <- server.Serve(listener) }()
	path := "/" + id.Token + "/clash.yaml"
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8080"+path, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("subscription readiness check failed: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil || resp.StatusCode != http.StatusOK || string(body) != string(content) {
		return errors.New("subscription readiness check failed")
	}
	select {
	case <-ctx.Done():
		return nil
	case <-exited:
		return fmt.Errorf("sing-box exited unexpectedly: %v", processErr)
	case err := <-httpDone:
		return fmt.Errorf("subscription service exited unexpectedly: %v", err)
	default:
	}
	fmt.Fprintf(output, "Services ready. Subscription URL: %s%s\n", cfg.SubBaseURL, path)
	select {
	case <-ctx.Done():
		return nil
	case <-exited:
		return fmt.Errorf("sing-box exited unexpectedly: %v", processErr)
	case err := <-httpDone:
		return fmt.Errorf("subscription service exited unexpectedly: %v", err)
	}
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Getenv, "/data", "sing-box", os.Stdout); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
