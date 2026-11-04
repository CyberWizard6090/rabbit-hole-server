package safehttp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"
)

const (
	DefaultTimeout      = 10 * time.Second
	DefaultMaxRedirects = 3
	DefaultUserAgent    = "RabbitHole-Fetcher/1.0"
)

var (
	ErrForbiddenURL     = errors.New("safehttp: the url is not allowed")
	ErrForbiddenAddress = errors.New("safehttp: the address is not allowed")
	ErrTooManyRedirects = errors.New("safehttp: too many redirects")
	ErrBodyTooLarge     = errors.New("safehttp: the response is too large")
	ErrBadResponse      = errors.New("safehttp: the response is not acceptable")
	ErrUnreachable      = errors.New("safehttp: the remote cannot be reached")
)

type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

type Client struct {
	http          *http.Client
	resolver      Resolver
	dial          DialFunc
	ports         []string
	allowAnyPort  bool
	allowLoopback bool
	timeout       time.Duration
	maxRedirects  int
	userAgent     string
	tlsConfig     *tls.Config
}

type Option func(*Client)

func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.timeout = d
		}
	}
}

func WithMaxRedirects(n int) Option {
	return func(c *Client) {
		if n >= 0 {
			c.maxRedirects = n
		}
	}
}

func WithAllowedPorts(ports ...int) Option {
	return func(c *Client) {
		c.ports = nil
		for _, p := range ports {
			c.ports = append(c.ports, strconv.Itoa(p))
		}
	}
}

func WithResolver(r Resolver) Option {
	return func(c *Client) {
		if r != nil {
			c.resolver = r
		}
	}
}

func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

func WithDialer(d DialFunc) Option {
	return func(c *Client) {
		if d != nil {
			c.dial = d
		}
	}
}

func WithTLSConfig(cfg *tls.Config) Option {
	return func(c *Client) { c.tlsConfig = cfg }
}

func AllowLoopbackForTests() Option {
	return func(c *Client) {
		c.allowLoopback = true
		c.allowAnyPort = true
	}
}

func New(opts ...Option) *Client {
	c := &Client{
		resolver:     net.DefaultResolver,
		ports:        []string{"80", "443"},
		timeout:      DefaultTimeout,
		maxRedirects: DefaultMaxRedirects,
		userAgent:    DefaultUserAgent,
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	c.dial = dialer.DialContext
	for _, opt := range opts {
		opt(c)
	}

	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            c.dialPublic,
		TLSClientConfig:        c.tlsConfig,
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  c.timeout,
		DisableCompression:     true,
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 64 << 10,
	}
	c.http = &http.Client{
		Transport:     transport,
		Jar:           nil,
		Timeout:       c.timeout,
		CheckRedirect: c.checkRedirect,
	}
	return c
}

func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > c.maxRedirects {
		return ErrTooManyRedirects
	}
	if _, err := c.validateParsed(req.URL); err != nil {
		return err
	}
	if len(via) > 0 && via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return ErrForbiddenURL
	}
	return nil
}

func (c *Client) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if addr, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{addr}, nil
	}
	return c.resolver.LookupNetIP(ctx, "ip", normalizeHost(host))
}

func (c *Client) addressAllowed(addr netip.Addr) bool {
	if IsPublicAddr(addr) {
		return true
	}
	return c.allowLoopback && addr.Zone() == "" && isLoopback(addr)
}

func (c *Client) dialPublic(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrForbiddenURL
	}
	if !c.portAllowed(port) {
		return nil, ErrForbiddenURL
	}
	addrs, err := c.resolve(ctx, host)
	if err != nil || len(addrs) == 0 {
		return nil, ErrUnreachable
	}
	for _, addr := range addrs {
		if !c.addressAllowed(addr) {
			return nil, ErrForbiddenAddress
		}
	}
	var lastErr error
	for _, addr := range addrs {
		conn, err := c.dial(ctx, network, net.JoinHostPort(addr.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, fmt.Errorf("%w: %w", ErrUnreachable, lastErr)
}

type Result struct {
	Body        []byte
	ContentType string
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if _, err := c.validateParsed(req.URL); err != nil {
		return nil, err
	}
	req = req.Clone(req.Context())
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Del("Cookie")
	return c.http.Do(req)
}

func (c *Client) Fetch(ctx context.Context, rawURL string, maxBytes int64, accept string) (*Result, error) {
	u, err := c.ValidateURL(rawURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ErrForbiddenURL
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, classify(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, ErrBadResponse
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" && enc != "identity" {
		return nil, ErrBadResponse
	}
	if resp.ContentLength > maxBytes {
		return nil, ErrBodyTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, classify(err)
	}
	if int64(len(body)) > maxBytes {
		return nil, ErrBodyTooLarge
	}
	return &Result{Body: body, ContentType: resp.Header.Get("Content-Type")}, nil
}

func classify(err error) error {
	for _, known := range []error{
		ErrForbiddenURL, ErrForbiddenAddress, ErrTooManyRedirects, ErrBodyTooLarge, ErrBadResponse, ErrUnreachable,
	} {
		if errors.Is(err, known) {
			return known
		}
	}
	return ErrUnreachable
}
