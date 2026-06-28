package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func clientWithProxy(proxyRaw string) (*http.Client, error) {
	client := *httpClient
	if strings.TrimSpace(proxyRaw) == "" {
		return &client, nil
	}
	transport, err := transportWithProxy(httpClient.Transport, proxyRaw)
	if err != nil {
		return nil, err
	}
	client.Transport = transport
	return &client, nil
}

func clientWithTimeout(timeoutSecs int, proxyRaw string) (*http.Client, error) {
	client, err := clientWithProxy(proxyRaw)
	if err != nil {
		return nil, err
	}
	client.Timeout = timeoutDuration(timeoutSecs)
	return client, nil
}

func transportWithProxy(base http.RoundTripper, proxyRaw string) (http.RoundTripper, error) {
	proxyURL, err := parseProxyURL(proxyRaw)
	if err != nil {
		return nil, err
	}
	transport := cloneHTTPTransport(base)
	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)
	case "socks4", "socks4a":
		transport.Proxy = nil
		transport.DialContext = socks4DialContext(proxyURL)
	case "socks5", "socks5h":
		transport.Proxy = nil
		transport.DialContext = socks5DialContext(proxyURL)
	default:
		return nil, unsupportedProxySchemeError()
	}
	return transport, nil
}

func cloneHTTPTransport(base http.RoundTripper) *http.Transport {
	if transport, ok := base.(*http.Transport); ok && transport != nil {
		return transport.Clone()
	}
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return &http.Transport{}
}

func parseProxyURL(proxyRaw string) (*url.URL, error) {
	proxyRaw = strings.TrimSpace(proxyRaw)
	if proxyRaw == "" {
		return nil, nil
	}
	proxyURL, err := url.Parse(proxyRaw)
	if err != nil {
		return nil, fmt.Errorf("--proxy must be a valid proxy URL")
	}
	proxyURL.Scheme = strings.ToLower(proxyURL.Scheme)
	if proxyURL.Scheme == "" || proxyURL.Host == "" || proxyURL.Hostname() == "" {
		return nil, fmt.Errorf("--proxy must include a scheme and host, for example http://127.0.0.1:8080")
	}
	switch proxyURL.Scheme {
	case "http", "https", "socks4", "socks4a", "socks5", "socks5h":
		return proxyURL, nil
	default:
		return nil, unsupportedProxySchemeError()
	}
}

func unsupportedProxySchemeError() error {
	return fmt.Errorf("--proxy scheme must be one of: http, https, socks4, socks4a, socks5, socks5h")
}

func proxyAddress(proxyURL *url.URL) string {
	port := proxyURL.Port()
	if port == "" {
		switch strings.ToLower(proxyURL.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			port = "1080"
		}
	}
	return net.JoinHostPort(proxyURL.Hostname(), port)
}

func socks5DialContext(proxyURL *url.URL) func(context.Context, string, string) (net.Conn, error) {
	var dialer net.Dialer
	return func(ctx context.Context, network string, address string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, proxyAddress(proxyURL))
		if err != nil {
			return nil, err
		}
		if err := applyContextDeadline(ctx, conn); err != nil {
			conn.Close()
			return nil, err
		}
		if err := socks5Handshake(conn, address, proxyURL); err != nil {
			conn.Close()
			return nil, err
		}
		_ = conn.SetDeadline(time.Time{})
		return conn, nil
	}
}

func socks5Handshake(conn net.Conn, targetAddress string, proxyURL *url.URL) error {
	methods := []byte{0x00}
	if proxyURL.User != nil {
		methods = append(methods, 0x02)
	}
	if _, err := conn.Write(append([]byte{0x05, byte(len(methods))}, methods...)); err != nil {
		return err
	}
	response := make([]byte, 2)
	if _, err := io.ReadFull(conn, response); err != nil {
		return err
	}
	if response[0] != 0x05 {
		return fmt.Errorf("socks5 proxy returned invalid version")
	}
	switch response[1] {
	case 0x00:
	case 0x02:
		if err := socks5UsernamePasswordAuth(conn, proxyURL); err != nil {
			return err
		}
	case 0xff:
		return fmt.Errorf("socks5 proxy requires an unsupported authentication method")
	default:
		return fmt.Errorf("socks5 proxy selected unsupported authentication method")
	}

	host, portRaw, err := net.SplitHostPort(targetAddress)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("target address has invalid port")
	}
	request, err := socks5ConnectRequest(host, port)
	if err != nil {
		return err
	}
	if _, err := conn.Write(request); err != nil {
		return err
	}
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	if header[0] != 0x05 {
		return fmt.Errorf("socks5 proxy returned invalid response version")
	}
	if header[1] != 0x00 {
		return fmt.Errorf("socks5 proxy connect failed: %s", socks5ReplyMessage(header[1]))
	}
	switch header[3] {
	case 0x01:
		_, err = io.CopyN(io.Discard, conn, 4)
	case 0x03:
		length := []byte{0}
		if _, err = io.ReadFull(conn, length); err != nil {
			return err
		}
		_, err = io.CopyN(io.Discard, conn, int64(length[0]))
	case 0x04:
		_, err = io.CopyN(io.Discard, conn, 16)
	default:
		return fmt.Errorf("socks5 proxy returned invalid address type")
	}
	if err != nil {
		return err
	}
	_, err = io.CopyN(io.Discard, conn, 2)
	return err
}

func socks5UsernamePasswordAuth(conn net.Conn, proxyURL *url.URL) error {
	if proxyURL.User == nil {
		return fmt.Errorf("socks5 proxy requested username/password authentication but --proxy has no credentials")
	}
	username := proxyURL.User.Username()
	password, _ := proxyURL.User.Password()
	if len(username) > 255 || len(password) > 255 {
		return fmt.Errorf("socks5 username and password must be at most 255 bytes")
	}
	request := []byte{0x01, byte(len(username))}
	request = append(request, []byte(username)...)
	request = append(request, byte(len(password)))
	request = append(request, []byte(password)...)
	if _, err := conn.Write(request); err != nil {
		return err
	}
	response := make([]byte, 2)
	if _, err := io.ReadFull(conn, response); err != nil {
		return err
	}
	if response[0] != 0x01 || response[1] != 0x00 {
		return fmt.Errorf("socks5 username/password authentication failed")
	}
	return nil
}

func socks5ConnectRequest(host string, port int) ([]byte, error) {
	request := []byte{0x05, 0x01, 0x00}
	ip := net.ParseIP(host)
	ip4 := ip.To4()
	ip16 := ip.To16()
	switch {
	case ip4 != nil:
		request = append(request, 0x01)
		request = append(request, ip4...)
	case ip16 != nil:
		request = append(request, 0x04)
		request = append(request, ip16...)
	default:
		host = strings.TrimSuffix(host, ".")
		if len(host) == 0 || len(host) > 255 {
			return nil, fmt.Errorf("target hostname must be 1-255 bytes for socks5")
		}
		request = append(request, 0x03, byte(len(host)))
		request = append(request, []byte(host)...)
	}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(port))
	return append(request, portBytes...), nil
}

func socks5ReplyMessage(code byte) string {
	switch code {
	case 0x01:
		return "general failure"
	case 0x02:
		return "connection not allowed"
	case 0x03:
		return "network unreachable"
	case 0x04:
		return "host unreachable"
	case 0x05:
		return "connection refused"
	case 0x06:
		return "ttl expired"
	case 0x07:
		return "command not supported"
	case 0x08:
		return "address type not supported"
	default:
		return "unknown error"
	}
}

func socks4DialContext(proxyURL *url.URL) func(context.Context, string, string) (net.Conn, error) {
	var dialer net.Dialer
	return func(ctx context.Context, network string, address string) (net.Conn, error) {
		conn, err := dialer.DialContext(ctx, network, proxyAddress(proxyURL))
		if err != nil {
			return nil, err
		}
		if err := applyContextDeadline(ctx, conn); err != nil {
			conn.Close()
			return nil, err
		}
		if err := socks4Handshake(ctx, conn, address, proxyURL); err != nil {
			conn.Close()
			return nil, err
		}
		_ = conn.SetDeadline(time.Time{})
		return conn, nil
	}
}

func socks4Handshake(ctx context.Context, conn net.Conn, targetAddress string, proxyURL *url.URL) error {
	host, portRaw, err := net.SplitHostPort(targetAddress)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("target address has invalid port")
	}
	ip := net.ParseIP(host).To4()
	useSocks4A := strings.EqualFold(proxyURL.Scheme, "socks4a")
	if ip == nil && !useSocks4A {
		ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
		if err != nil {
			return err
		}
		for _, resolved := range ips {
			if ip = resolved.To4(); ip != nil {
				break
			}
		}
		if ip == nil {
			return fmt.Errorf("socks4 requires an IPv4 target")
		}
	}
	if ip == nil {
		ip = net.IPv4(0, 0, 0, 1).To4()
	}

	userID := ""
	if proxyURL.User != nil {
		userID = proxyURL.User.Username()
		if password, ok := proxyURL.User.Password(); ok && password != "" {
			userID += ":" + password
		}
	}
	request := []byte{0x04, 0x01}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(port))
	request = append(request, portBytes...)
	request = append(request, ip...)
	request = append(request, []byte(userID)...)
	request = append(request, 0x00)
	if useSocks4A && net.ParseIP(host) == nil {
		request = append(request, []byte(host)...)
		request = append(request, 0x00)
	}
	if _, err := conn.Write(request); err != nil {
		return err
	}
	response := make([]byte, 8)
	if _, err := io.ReadFull(conn, response); err != nil {
		return err
	}
	if response[0] != 0x00 {
		return fmt.Errorf("socks4 proxy returned invalid response")
	}
	if response[1] != 0x5a {
		return fmt.Errorf("socks4 proxy connect failed: %s", socks4ReplyMessage(response[1]))
	}
	return nil
}

func socks4ReplyMessage(code byte) string {
	switch code {
	case 0x5b:
		return "request rejected or failed"
	case 0x5c:
		return "client is not running identd"
	case 0x5d:
		return "client identd user ID mismatch"
	default:
		return "unknown error"
	}
}

func applyContextDeadline(ctx context.Context, conn net.Conn) error {
	if deadline, ok := ctx.Deadline(); ok {
		return conn.SetDeadline(deadline)
	}
	return nil
}
