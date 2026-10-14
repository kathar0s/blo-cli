// Package client speaks Billing-On's internal AJAX protocol: it logs in
// (RSA-encrypted password), maintains the session cookie, and issues the
// AES-encrypted ("encBody") POST requests the dashboard uses. See ANALYSIS.md.
package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/kathar0s/blo-cli/internal/auth"
	"github.com/kathar0s/blo-cli/internal/cache"
	"github.com/kathar0s/blo-cli/internal/config"
	"github.com/kathar0s/blo-cli/internal/crypto"
)

const userAgent = "billing-on-cli/0.1 (+https://github.com/kathar0s/blo-cli)"

// Client wraps an authenticated HTTP session against Billing-On.
type Client struct {
	http       *http.Client // normal AJAX calls (short timeout)
	download   *http.Client // file/CSV export (long timeout for large bodies)
	base       string
	csrfHeader string
	csrfToken  string
	email      string // logged-in account; namespaces the response cache
}

// New builds a client with a cookie jar. It does not authenticate.
func New() (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	noRedirect := func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{
		http: &http.Client{
			Jar:           jar,
			Timeout:       60 * time.Second,
			CheckRedirect: noRedirect,
		},
		// Export bodies can be ~100MB; give them a generous timeout. Shares the
		// same cookie jar so the authenticated session carries over.
		download: &http.Client{
			Jar:           jar,
			Timeout:       10 * time.Minute,
			CheckRedirect: noRedirect,
		},
		base: strings.TrimRight(config.BaseURL(), "/"),
	}, nil
}

// Restore rebuilds an authenticated client from a saved session.
func Restore(s *auth.Session) (*Client, error) {
	c, err := New()
	if err != nil {
		return nil, err
	}
	c.http.Jar.SetCookies(c.cookieURL(), s.ToHTTPCookies())
	c.csrfHeader = s.CSRFHeader
	c.csrfToken = s.CSRFToken
	c.email = s.Email
	return c, nil
}

// cookieURL returns a URL under the /billingon path so the cookie jar matches
// the session cookies the server sets with Path=/billingon. Querying the jar
// with the bare base URL (path "/") would miss them.
func (c *Client) cookieURL() *url.URL {
	u, _ := url.Parse(c.base + "/billingon/")
	return u
}

var (
	reCSRFInput  = regexp.MustCompile(`(?i)<input[^>]*name="_csrf"[^>]*value="([^"]+)"`)
	reCSRFMeta   = regexp.MustCompile(`(?i)<meta[^>]*name="_csrf"[^>]*content="([^"]+)"`)
	reCSRFHeader = regexp.MustCompile(`(?i)<meta[^>]*name="_csrf_header"[^>]*content="([^"]+)"`)
)

// authProcResp is the public-key + MFA flag handshake response.
type authProcResp struct {
	PubKeyModulus  string `json:"pubKeyModulus"`
	PubKeyExponent string `json:"pubKeyExponent"`
	Flag           string `json:"flag"`
	ErrMsg         string `json:"errMsg"`
}

// Login authenticates with email/password and returns a Session to persist.
func (c *Client) Login(email, password string) (*auth.Session, error) {
	// 1) Fetch the login page to obtain the form CSRF token + session cookie.
	loginHTML, err := c.get(c.base + "/billingon/login.do")
	if err != nil {
		return nil, fmt.Errorf("fetch login page: %w", err)
	}
	formCSRF := firstGroup(reCSRFInput, loginHTML)
	if formCSRF == "" {
		return nil, fmt.Errorf("could not find _csrf token on login page (site layout changed?)")
	}

	// 2) Handshake: get the RSA public key and MFA flag for this account.
	hsURL := c.base + "/billingon/login/authProcFlag.do?adminId=" + url.QueryEscape(email)
	hsBody, err := c.get(hsURL)
	if err != nil {
		return nil, fmt.Errorf("auth handshake: %w", err)
	}
	var hs authProcResp
	if err := json.Unmarshal([]byte(hsBody), &hs); err != nil {
		return nil, fmt.Errorf("auth handshake: unexpected response: %s", truncate(hsBody, 200))
	}
	switch hs.Flag {
	case "0":
		return nil, fmt.Errorf("로그인 실패: 존재하지 않는 아이디이거나 잘못된 계정입니다")
	case "N":
		// no MFA — proceed
	case "":
		if hs.ErrMsg != "" {
			return nil, fmt.Errorf("로그인 실패: %s", hs.ErrMsg)
		}
		return nil, fmt.Errorf("로그인 실패: 공개키 응답이 비어 있습니다")
	default:
		return nil, fmt.Errorf("이 계정은 MFA(2단계 인증)가 설정되어 있어 현재 CLI 로그인을 지원하지 않습니다 (flag=%s)", hs.Flag)
	}
	if hs.PubKeyModulus == "" || hs.PubKeyExponent == "" {
		return nil, fmt.Errorf("로그인 실패: 서버가 RSA 공개키를 제공하지 않았습니다")
	}

	// 3) RSA-encrypt the password.
	securedPw, err := crypto.RSAEncryptPassword(hs.PubKeyModulus, hs.PubKeyExponent, password)
	if err != nil {
		return nil, fmt.Errorf("encrypt password: %w", err)
	}

	// 4) Submit the login form. Success is a 302 redirect to /main.do.
	form := url.Values{
		"_csrf":     {formCSRF},
		"vResponse": {""},
		"adminId":   {email},
		"passwd":    {securedPw},
	}
	resp, err := c.postForm(c.base+"/billingon/login/loginUser.do", form, nil)
	if err != nil {
		return nil, fmt.Errorf("submit login: %w", err)
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusFound || strings.Contains(loc, "login") {
		return nil, fmt.Errorf("로그인 실패: 아이디 또는 비밀번호를 확인하세요 (status=%d, loc=%s)", resp.StatusCode, loc)
	}

	// 5) Load the authenticated dashboard shell to capture the AJAX CSRF token.
	// The token lives in a <meta name="_csrf"> tag on the dashboard view; the
	// header name defaults to X-CSRF-TOKEN (confirmed from captured traffic).
	shell, err := c.get(c.base + "/billingon/init.do?menuUrl=/dashboard/dashboardCustomerView.do&pMenuCd=dashboard&menuCd=custom")
	if err == nil {
		if t := firstGroup(reCSRFMeta, shell); t != "" {
			c.csrfToken = t
		}
		if h := firstGroup(reCSRFHeader, shell); h != "" {
			c.csrfHeader = h
		}
	}
	if c.csrfHeader == "" {
		c.csrfHeader = "X-CSRF-TOKEN"
	}

	sess := &auth.Session{Email: email, CSRFHeader: c.csrfHeader, CSRFToken: c.csrfToken}
	sess.FromHTTPCookies(c.http.Jar.Cookies(c.cookieURL()))
	return sess, nil
}

// Call issues an encrypted AJAX request to the given action path (e.g.
// "/billingon/dashboard/customerCostSummaryAjax.do") with JSON params, and
// decodes the plaintext JSON response into out.
//
// originalMethod is the HTTP method the endpoint is actually mapped to on the
// server ("GET" or "POST"). The request is always sent as POST with an
// encrypted body, and the server restores the real method from the
// _originalMethod field to route the request — sending the wrong one yields
// HTTP 405. The correct value differs per endpoint (see internal/billing).
func (c *Client) Call(action, originalMethod string, params map[string]string, out any) error {
	body, err := c.callRaw(action, originalMethod, params)
	if err != nil {
		return err
	}
	return decode(action, body, out)
}

// CallCached is Call with an on-disk response cache. ttl follows cache.Get
// semantics: 0 = no cache (behaves exactly like Call), >0 = cache for that
// duration, <0 = cache permanently. The cache key covers the account, endpoint,
// method, and params, so distinct queries never collide. A live fetch on a miss
// is written back before decoding.
func (c *Client) CallCached(action, originalMethod string, params map[string]string, out any, ttl time.Duration) error {
	if ttl == 0 {
		return c.Call(action, originalMethod, params, out)
	}
	key := c.cacheKey(action, originalMethod, params)
	if body, ok := cache.Get(key, ttl); ok {
		return decode(action, body, out)
	}
	body, err := c.callRaw(action, originalMethod, params)
	if err != nil {
		return err
	}
	cache.Put(key, body, ttl)
	return decode(action, body, out)
}

// callRaw issues the encrypted AJAX request and returns the plaintext response
// body, leaving JSON decoding to the caller (so cached and live paths share it).
func (c *Client) callRaw(action, originalMethod string, params map[string]string) ([]byte, error) {
	paramJSON, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	encBody, err := crypto.EncBody(string(paramJSON))
	if err != nil {
		return nil, err
	}
	if originalMethod == "" {
		originalMethod = "POST"
	}
	form := url.Values{
		"encBody":         {encBody},
		"_originalMethod": {originalMethod},
	}
	resp, err := c.postForm(c.base+action, form, map[string]string{
		"X-Requested-With": "XMLHttpRequest",
		"AJAX":             "true",
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// A redirect to the login page means the session expired.
	if resp.StatusCode == http.StatusFound || looksLikeLogin(body) {
		return nil, ErrSessionExpired
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d: %s", action, resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

func decode(action string, body []byte, out any) error {
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("%s: decode response: %w (body: %s)", action, err, truncate(string(body), 200))
		}
	}
	return nil
}

// cacheKey derives the response-cache key for an encrypted AJAX call. params is
// marshaled with encoding/json, which sorts map keys, so the key is stable.
func (c *Client) cacheKey(action, method string, params map[string]string) string {
	pj, _ := json.Marshal(params)
	return cache.Key(c.email, c.base, action, method, string(pj))
}

// DownloadForm submits a plain (unencrypted) form POST and returns the raw
// response body. Used for the Excel/CSV export endpoint, which streams a file
// attachment rather than JSON and is NOT part of the encBody scheme. The _csrf
// token is injected as a form field automatically.
func (c *Client) DownloadForm(action string, form url.Values) ([]byte, error) {
	if c.csrfToken != "" && form.Get("_csrf") == "" {
		form.Set("_csrf", c.csrfToken)
	}
	resp, err := c.postFormWith(c.download, c.base+action, form, map[string]string{
		"X-Requested-With": "XMLHttpRequest",
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// An expired session redirects (302) to the login page instead of streaming.
	if resp.StatusCode == http.StatusFound || looksLikeLogin(body) {
		return nil, ErrSessionExpired
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d: %s", action, resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

// DownloadFormCached is DownloadForm with an on-disk cache (same ttl semantics
// as CallCached). The _csrf field is excluded from the cache key because it
// rotates per session and would otherwise bust the cache on every login.
func (c *Client) DownloadFormCached(action string, form url.Values, ttl time.Duration) ([]byte, error) {
	if ttl == 0 {
		return c.DownloadForm(action, form)
	}
	key := c.downloadCacheKey(action, form)
	if body, ok := cache.Get(key, ttl); ok {
		return body, nil
	}
	body, err := c.DownloadForm(action, form)
	if err != nil {
		return nil, err
	}
	cache.Put(key, body, ttl)
	return body, nil
}

func (c *Client) downloadCacheKey(action string, form url.Values) string {
	f := make(url.Values, len(form))
	for k, v := range form {
		if k == "_csrf" {
			continue
		}
		f[k] = v
	}
	return cache.Key(c.email, c.base, action, "FORM", f.Encode())
}

// ErrSessionExpired signals the caller should re-authenticate.
var ErrSessionExpired = fmt.Errorf("session expired; run `blo auth login` again")

func looksLikeLogin(body []byte) bool {
	s := string(body)
	return strings.Contains(s, "loginUserForm") || strings.Contains(s, "/login.do")
}

// --- low-level helpers ---

func (c *Client) get(u string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	c.applyCommonHeaders(req, false)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func (c *Client) postForm(u string, form url.Values, extra map[string]string) (*http.Response, error) {
	return c.postFormWith(c.http, u, form, extra)
}

func (c *Client) postFormWith(hc *http.Client, u string, form url.Values, extra map[string]string) (*http.Response, error) {
	encoded := form.Encode()
	// All portal AJAX calls are read-only queries, so retrying on a transient
	// connection error is safe. The "server closed idle connection" error in
	// particular (Go reused a keep-alive connection the server had just dropped)
	// means the request never reached the server — common when we fan out several
	// concurrent calls after the client has been idle. Retry a couple of times.
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest(http.MethodPost, u, strings.NewReader(encoded))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
		c.applyCommonHeaders(req, true)
		for k, v := range extra {
			req.Header.Set(k, v)
		}
		if c.csrfHeader != "" && c.csrfToken != "" {
			req.Header.Set(c.csrfHeader, c.csrfToken)
		}
		resp, err := hc.Do(req)
		if err == nil || !isRetryable(err) {
			return resp, err
		}
		lastErr = err
	}
	return nil, lastErr
}

// isRetryable reports whether a request error is a transient connection failure
// worth retrying. These all indicate the connection dropped around the request
// rather than the server rejecting it.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, s := range []string{
		"server closed idle connection",
		"connection reset by peer",
		"broken pipe",
		"EOF",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func (c *Client) applyCommonHeaders(req *http.Request, ajax bool) {
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Origin", c.base)
	req.Header.Set("Referer", c.base+"/billingon/main.do")
	if ajax {
		req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	}
}

func firstGroup(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
