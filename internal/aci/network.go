package aci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxNetworkFetchBytes = 256 << 10

type NetworkFetchResult struct {
	StatusCode  int    `json:"status_code"`
	ContentType string `json:"content_type,omitempty"`
	Body        string `json:"body"`
	Truncated   bool   `json:"truncated,omitempty"`
}

func (r *Runtime) NetworkFetch(ctx context.Context, rawURL string) (NetworkFetchResult, error) {
	target, err := validateOutboundURL(rawURL)
	if err != nil {
		return NetworkFetchResult{}, err
	}
	timeout := r.cfg.CommandTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	maxBytes := r.cfg.MaxCommandOutputBytes
	if maxBytes <= 0 || maxBytes > maxNetworkFetchBytes {
		maxBytes = maxNetworkFetchBytes
	}

	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(dialCtx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("split network target: %w", err)
			}
			ips, err := net.DefaultResolver.LookupIPAddr(dialCtx, host)
			if err != nil {
				return nil, fmt.Errorf("resolve network target: %w", err)
			}
			if len(ips) == 0 {
				return nil, errors.New("network target resolved to no addresses")
			}
			for _, resolved := range ips {
				if !isPublicNetworkIP(resolved.IP) {
					return nil, fmt.Errorf("network target resolved to disallowed address %s", resolved.IP.String())
				}
			}
			return dialer.DialContext(dialCtx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("network fetch exceeded redirect limit")
			}
			_, err := validateOutboundURL(req.URL.String())
			return err
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return NetworkFetchResult{}, err
	}
	req.Header.Set("User-Agent", "MAR-bounded-network-fetch/1")
	resp, err := client.Do(req)
	if err != nil {
		return NetworkFetchResult{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
	if err != nil {
		return NetworkFetchResult{}, err
	}
	truncated := len(body) > maxBytes
	if truncated {
		body = body[:maxBytes]
	}
	return NetworkFetchResult{
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Body:        string(body),
		Truncated:   truncated,
	}, nil
}

func validateOutboundURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("network URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse network URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("network fetch supports only http and https")
	}
	if u.User != nil {
		return nil, errors.New("network URL userinfo is not allowed")
	}
	if u.Hostname() == "" {
		return nil, errors.New("network URL hostname is required")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		host == "metadata" || host == "metadata.google.internal" ||
		host == "instance-data.ec2.internal" || strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".localdomain") || strings.HasSuffix(host, ".internal") ||
		strings.HasSuffix(host, ".home.arpa") {
		return nil, errors.New("network URL targets a local or metadata-style hostname")
	}
	if ip := net.ParseIP(host); ip != nil && !isPublicNetworkIP(ip) {
		return nil, errors.New("network URL targets a private, local, or special-use address")
	}
	return u, nil
}

func isPublicNetworkIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1]&0xc0 == 0x40 {
			return false
		}
		if v4[0] == 0 || (v4[0] == 169 && v4[1] == 254) {
			return false
		}
	}
	return true
}
