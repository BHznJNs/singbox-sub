package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const defaultRealityServer = "www.microsoft.com"

type settings struct {
	ServerAddr string
	ServerPort int
	ServerName string
	SubBaseURL string
}

func validHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`).MatchString(label) {
			return false
		}
	}
	return true
}

func readSettings(getenv func(string) string) (settings, error) {
	cfg := settings{getenv("SERVER_ADDR"), 24443, defaultRealityServer, "http://localhost:8080"}
	if !validHost(cfg.ServerAddr) {
		return settings{}, errors.New("SERVER_ADDR must be an IP address or hostname without scheme or port")
	}
	if value := getenv("SERVER_PORT"); value != "" {
		if !regexp.MustCompile(`^[0-9]+$`).MatchString(value) {
			return settings{}, errors.New("SERVER_PORT must be an integer from 1 to 65535, excluding 8080")
		}
		port, err := strconv.Atoi(value)
		if err != nil || port < 1 || port > 65535 || port == 8080 {
			return settings{}, errors.New("SERVER_PORT must be an integer from 1 to 65535, excluding 8080")
		}
		cfg.ServerPort = port
	}
	if value := getenv("REALITY_SERVER_NAME"); value != "" {
		if !validHost(value) || net.ParseIP(value) != nil {
			return settings{}, errors.New("REALITY_SERVER_NAME must be a hostname")
		}
		cfg.ServerName = value
	}
	if value := getenv("SUB_BASE_URL"); value != "" {
		u, err := url.Parse(value)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !validHost(u.Hostname()) || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || strings.ContainsAny(value, "\r\n\t ") {
			return settings{}, errors.New("SUB_BASE_URL must be an HTTP(S) URL without credentials, query or fragment")
		}
		if u.Port() != "" {
			port, err := strconv.Atoi(u.Port())
			if err != nil || port < 1 || port > 65535 {
				return settings{}, errors.New("SUB_BASE_URL has an invalid port")
			}
		}
		cfg.SubBaseURL = strings.TrimRight(value, "/")
	}
	return cfg, nil
}

func serverConfig(cfg settings, id identity) ([]byte, error) {
	return json.MarshalIndent(map[string]any{
		"log": map[string]any{"level": "info", "timestamp": true},
		"inbounds": []any{map[string]any{
			"type": "vless", "tag": "vless-in", "listen": "0.0.0.0", "listen_port": cfg.ServerPort,
			"users": []any{map[string]any{"name": "user", "uuid": id.UUID, "flow": "xtls-rprx-vision"}},
			"tls": map[string]any{"enabled": true, "server_name": cfg.ServerName,
				"reality": map[string]any{"enabled": true, "private_key": id.PrivateKey, "short_id": []string{id.ShortID},
					"handshake": map[string]any{"server": cfg.ServerName, "server_port": 443}}},
		}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route":     map[string]any{"final": "direct"},
	}, "", "  ")
}

func subscription(cfg settings, id identity) []byte {
	quote := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	return []byte(fmt.Sprintf(`mixed-port: 7890
allow-lan: false
mode: rule
log-level: info
ipv6: false
dns:
  enable: true
  ipv6: false
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  default-nameserver:
    - 1.1.1.1
    - 8.8.8.8
  nameserver:
    - https://1.1.1.1/dns-query
    - https://8.8.8.8/dns-query
proxies:
  - name: "VLESS-REALITY"
    type: vless
    server: %s
    port: %d
    uuid: %s
    network: tcp
    udp: true
    tls: true
    flow: xtls-rprx-vision
    servername: %s
    client-fingerprint: chrome
    packet-encoding: xudp
    reality-opts:
      public-key: %s
      short-id: %s
proxy-groups:
  - name: "PROXY"
    type: select
    proxies:
      - "VLESS-REALITY"
      - DIRECT
rules:
  - IP-CIDR,0.0.0.0/8,DIRECT,no-resolve
  - IP-CIDR,10.0.0.0/8,DIRECT,no-resolve
  - IP-CIDR,100.64.0.0/10,DIRECT,no-resolve
  - IP-CIDR,127.0.0.0/8,DIRECT,no-resolve
  - IP-CIDR,169.254.0.0/16,DIRECT,no-resolve
  - IP-CIDR,172.16.0.0/12,DIRECT,no-resolve
  - IP-CIDR,192.168.0.0/16,DIRECT,no-resolve
  - IP-CIDR6,::1/128,DIRECT,no-resolve
  - IP-CIDR6,fc00::/7,DIRECT,no-resolve
  - IP-CIDR6,fe80::/10,DIRECT,no-resolve
  - GEOSITE,cn,DIRECT
  - GEOIP,CN,DIRECT
  - MATCH,PROXY
`, quote(cfg.ServerAddr), cfg.ServerPort, quote(id.UUID), quote(cfg.ServerName), quote(id.PublicKey), quote(id.ShortID)))
}
