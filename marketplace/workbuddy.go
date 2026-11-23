package marketplace

// workbuddy.go — WorkBuddy OAuth credential support for seller channels.
//
// A WorkBuddy channel's upstream credential is not a static API key: it is a
// browser sign-in captured through WorkBuddy's own plugin auth flow
// (POST /v2/plugin/auth/state -> open the returned Tencent sign-in page ->
// poll /v2/plugin/auth/token?state= until the user signs in). The captured
// bundle {access, refresh, expires, uid, domain, enterprise_id} is stored
// encrypted on the listing; the listing's KeyCiphertext keeps only the
// current access token so every existing key path (probes, relay, patrol)
// works unchanged. Access tokens live about an hour; refresh tokens about
// 340 days.
//
// Chats must be streamed (WorkBuddy refuses non-stream chat requests) and
// carry the desktop CLI's header fingerprint: a User-Agent not starting
// "WorkBuddy/<version>" is refused with error 10085. The static headers are
// written into the channel's header_override at create time; the dynamic
// conversation/request ids ride the {uuid} placeholder.

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
)

// VendorWorkBuddy is the channel slug of the built-in WorkBuddy plan channel.
const VendorWorkBuddy = "workbuddy"

// WorkBuddy China build constants, matching the desktop client the plugin
// emulates (opencode-workbuddy-auth 0.2.36): a UA outside WorkBuddy/<app>
// draws error 10085.
const (
	wbEndpoint    = "https://copilot.tencent.com"
	wbChatBase    = "https://copilot.tencent.com/v2"
	wbMsgsBase    = "https://copilot.tencent.com/v1"
	wbPlatform    = "workbuddy"
	wbAppVersion  = "5.9.3"
	wbCLIVersion  = "2.165.0"
	wbAppName     = "WorkBuddy"
	wbOpenAISDK   = "6.25.0"
	wbNodeVersion = "v22.21.1"
	wbAgent       = "WorkBuddy/5.9.3 WorkBuddy/5.9.3 CLI/2.165.0"
)

const (
	wbAuthSessionTTL   = 5 * time.Minute // WorkBuddy's own sign-in poll window
	wbAuthPollTimeout  = 30 * time.Second
	wbRefreshEarlyMs   = 60 * 1000 // refresh a token this close to its end
	wbRenewedKeepMs    = 60 * 1000 // reuse a just-finished refresh result
	wbSignInPendingErr = 11217     // WorkBuddy's "not signed in yet" code
)

// wbCredential is the encrypted OAuth bundle of a WorkBuddy channel.
// Times are unix milliseconds.
type wbCredential struct {
	Access           string `json:"access"`
	Refresh          string `json:"refresh"`
	Expires          int64  `json:"expires"`
	RefreshExpiresAt int64  `json:"refresh_expires_at,omitempty"`
	Domain           string `json:"domain"`
	TokenType        string `json:"token_type,omitempty"`
	UID              string `json:"uid"`
	EnterpriseID     string `json:"enterprise_id,omitempty"`
	AccountName      string `json:"account_name,omitempty"`
}

func (c *wbCredential) stale(nowMs int64) bool {
	return c.Expires > 0 && nowMs >= c.Expires-wbRefreshEarlyMs
}

func (c *wbCredential) refreshable(nowMs int64) bool {
	return c.Refresh != "" && !(c.RefreshExpiresAt > 0 && nowMs >= c.RefreshExpiresAt)
}

func (c *wbCredential) domain() string {
	if c.Domain != "" {
		return c.Domain
	}
	return "copilot.tencent.com"
}

// wbIdentityFingerprint is the listing's stable credential identity: the
// account, never the token. Refreshing must not change the fingerprint or a
// banned account could be re-listed.
func wbIdentityFingerprint(c *wbCredential) (string, error) {
	return ListingKeyFingerprint("wb|" + c.UID + "|" + c.domain() + "|" + c.EnterpriseID)
}

// wbError is a WorkBuddy plugin-API failure: the {code,msg,data} envelope
// carried a nonzero code, or the HTTP status was not 2xx.
type wbError struct {
	Status int
	Code   int
	Msg    string
}

func (e *wbError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("workbuddy: code %d: %s", e.Code, e.Msg)
	}
	return fmt.Sprintf("workbuddy: http %d: %s", e.Status, e.Msg)
}

// wbPluginEndpoint is the plugin API origin. A variable so tests can point
// the whole flow at an httptest server.
var wbPluginEndpoint = wbEndpoint

// wbCall mirrors the plugin's call(): every WorkBuddy plugin endpoint answers
// {code,msg,data}; a nonzero code is an error carrying msg.
func wbCall(ctx context.Context, client *http.Client, method, path string, headers map[string]string, body any) (map[string]any, error) {
	return wbCallBase(ctx, client, method, wbPluginEndpoint, path, headers, body)
}

// wbCallBase is wbCall against an account-resolved base: a cloud-hosted
// enterprise answers on its own domain, not the shared plugin endpoint.
func wbCallBase(ctx context.Context, client *http.Client, method, base, path string, headers map[string]string, body any) (map[string]any, error) {
	var reader io.Reader
	if body != nil {
		raw, err := common.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "WorkBuddy/"+wbAppVersion)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var env struct {
		Code any            `json:"code"`
		Msg  string         `json:"msg"`
		Data map[string]any `json:"data"`
	}
	_ = common.Unmarshal(raw, &env)
	code := 0
	switch v := env.Code.(type) {
	case float64:
		code = int(v)
	case string:
		_, _ = fmt.Sscanf(v, "%d", &code)
	}
	if code != 0 {
		return nil, &wbError{Status: resp.StatusCode, Code: code, Msg: env.Msg}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &wbError{Status: resp.StatusCode, Msg: env.Msg}
	}
	return env.Data, nil
}

func wbNum(data map[string]any, key string) int64 {
	switch v := data[key].(type) {
	case float64:
		return int64(v)
	case string:
		var n int64
		_, _ = fmt.Sscanf(v, "%d", &n)
		return n
	}
	return 0
}

func wbStr(data map[string]any, key string) string {
	if s, ok := data[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// wbMergeToken folds a sign-in/refresh answer into the stored credential
// (expiresAt ms or expiresIn seconds; the plugin's merge()).
func wbMergeToken(c *wbCredential, got map[string]any) {
	now := time.Now().UnixMilli()
	if tok := wbStr(got, "accessToken"); tok != "" {
		c.Access = tok
	}
	if tok := wbStr(got, "refreshToken"); tok != "" {
		c.Refresh = tok
	}
	if d := wbStr(got, "domain"); d != "" {
		c.Domain = d
	}
	if t := wbStr(got, "tokenType"); t != "" {
		c.TokenType = t
	}
	if exp := wbNum(got, "expiresAt"); exp > 0 {
		c.Expires = exp
	} else if sec := wbNum(got, "expiresIn"); sec > 0 {
		c.Expires = now + sec*1000
	}
	if exp := wbNum(got, "refreshExpiresAt"); exp > 0 {
		c.RefreshExpiresAt = exp
	} else if sec := wbNum(got, "refreshExpiresIn"); sec > 0 {
		c.RefreshExpiresAt = now + sec*1000
	}
}

func wbEnterpriseHeaders(c *wbCredential) map[string]string {
	if strings.TrimSpace(c.EnterpriseID) == "" {
		return nil
	}
	return map[string]string{"X-Enterprise-Id": c.EnterpriseID, "X-Tenant-Id": c.EnterpriseID}
}

// wbCloudHostedRe are WorkBuddy's cloud-hosted enterprise domains (the
// client's product.json cloudHostedDomain). Such an account is served on
// its own host; Tencent's iOA domains match the pattern too but are served
// at the shared endpoint, so they are checked first.
var wbCloudHostedRe = []*regexp.Regexp{
	regexp.MustCompile(`^[a-z0-9-]+\.sso\.copilot\.tencent\.com$`),
	regexp.MustCompile(`^[a-z0-9-]+\.copilot\.qq\.com$`),
	regexp.MustCompile(`^[a-z0-9-]+\.sso\.codebuddy\.cn$`),
}

// wbAccountEndpoint mirrors the plugin's endpointOf: an account named with a
// cloud-hosted enterprise domain is served there; everyone else (and the iOA
// domains) uses the shared endpoint.
func wbAccountEndpoint(c *wbCredential) string {
	d := strings.ToLower(strings.TrimSpace(c.Domain))
	if d != "" && d != "tencent.sso.copilot.tencent.com" && d != "tencent.sso.codebuddy.cn" {
		for _, re := range wbCloudHostedRe {
			if re.MatchString(d) {
				return "https://" + d
			}
		}
	}
	return wbPluginEndpoint
}

// wbLiveModel is one model the account's product config lists for its CLI.
type wbLiveModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// wbLiveModels reads the account's callable models from WorkBuddy's product
// config (GET /v3/config), the same list WorkBuddy's own CLI offers: the
// "cli" agent's model ids, plus enterprise-owned "custom:" models that are
// appended to it. Models tagged "custom"/"chat" are a desktop client's own
// local entries and stay out.
func wbLiveModels(ctx context.Context, client *http.Client, cred *wbCredential) ([]wbLiveModel, error) {
	h := wbHeadersForProbe(cred)
	h["Authorization"] = "Bearer " + cred.Access
	data, err := wbCallBase(ctx, client, http.MethodGet,
		wbAccountEndpoint(cred), "/v3/config", h, nil)
	if err != nil {
		return nil, err
	}
	detail := map[string]map[string]any{}
	if rows, ok := data["models"].([]any); ok {
		for _, row := range rows {
			m, _ := row.(map[string]any)
			if id := wbStr(m, "id"); id != "" {
				detail[id] = m
			}
		}
	}
	var out []wbLiveModel
	seen := map[string]bool{}
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		name := id
		if d, ok := detail[id]; ok {
			if n := wbStr(d, "name"); n != "" {
				name = n
			}
		}
		out = append(out, wbLiveModel{ID: id, Name: name})
	}
	cliFound := false
	if agents, ok := data["agents"].([]any); ok {
		for _, row := range agents {
			a, _ := row.(map[string]any)
			if wbStr(a, "name") != "cli" {
				continue
			}
			cliFound = true
			if ids, ok := a["models"].([]any); ok {
				for _, v := range ids {
					if id, ok := v.(string); ok {
						add(id)
					}
				}
			}
		}
	}
	if cliFound {
		if rows, ok := data["models"].([]any); ok {
			for _, row := range rows {
				m, _ := row.(map[string]any)
				if id := wbStr(m, "id"); strings.HasPrefix(id, "custom:") {
					add(id)
				}
			}
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Sign-in sessions
// ---------------------------------------------------------------------------

type wbAuthSession struct {
	ID         string
	SellerID   int64
	State      string
	AuthURL    string
	Deadline   time.Time
	Status     string // pending / authorized / failed
	Err        string
	Account    string
	Models     []wbLiveModel
	Credential *wbCredential
}

var (
	wbAuthMu       sync.Mutex
	wbAuthSessions = map[string]*wbAuthSession{}
)

func wbAuthSessionFor(sellerID int64, id string) (*wbAuthSession, bool) {
	wbAuthMu.Lock()
	defer wbAuthMu.Unlock()
	s, ok := wbAuthSessions[id]
	if !ok || s.SellerID != sellerID {
		return nil, false
	}
	return s, true
}

// wbHTTPClient overrides the plugin-API client in tests; nil uses the egress
// verify client (dial-time public-IP validation still runs).
var wbHTTPClient *http.Client

// wbAuthClient is the HTTP client the sign-in flow uses. It is the verify
// egress client: host whitelist does not apply to fixed WorkBuddy endpoints,
// dial-time public-IP validation still runs.
func wbAuthClient() *http.Client {
	if wbHTTPClient != nil {
		return wbHTTPClient
	}
	return EgressVerifyHTTPClient()
}

// wbAuthStart begins a browser sign-in: WorkBuddy names the Tencent sign-in
// page and the state the later token poll binds to it.
func wbAuthStart(ctx context.Context, sellerID int64) (*wbAuthSession, error) {
	data, err := wbCall(ctx, wbAuthClient(), http.MethodPost,
		"/v2/plugin/auth/state?platform="+wbPlatform,
		map[string]string{
			"X-No-Authorization":   "true",
			"X-No-User-Id":         "true",
			"X-No-Enterprise-Id":   "true",
			"X-No-Department-Info": "true",
		}, map[string]any{})
	if err != nil {
		return nil, err
	}
	state := wbStr(data, "state")
	authURL := wbStr(data, "authUrl")
	if state == "" || authURL == "" || !strings.HasPrefix(authURL, "https://") {
		return nil, errors.New("workbuddy: sign-in gave no auth page")
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	sep := "?"
	if strings.Contains(authURL, "?") {
		sep = "&"
	}
	s := &wbAuthSession{
		ID:       hex.EncodeToString(b),
		SellerID: sellerID,
		State:    state,
		// version/loginSessionId ride along as the desktop app sends them.
		AuthURL:  authURL + sep + "version=" + wbAppVersion + "&loginSessionId=" + hex.EncodeToString(b),
		Deadline: time.Now().Add(wbAuthSessionTTL),
		Status:   "pending",
	}
	wbAuthMu.Lock()
	// Drop expired/abandoned sessions so the map cannot grow unbounded.
	for id, old := range wbAuthSessions {
		if old.Status != "pending" || time.Now().After(old.Deadline) {
			delete(wbAuthSessions, id)
		}
	}
	wbAuthSessions[s.ID] = s
	wbAuthMu.Unlock()
	return s, nil
}

// wbAuthPoll performs one round of the token poll. Each frontend status check
// drives a single WorkBuddy query, so the cadence stays the plugin's ~1/s.
func wbAuthPoll(ctx context.Context, sellerID int64, id string) (*wbAuthSession, error) {
	s, ok := wbAuthSessionFor(sellerID, id)
	if !ok {
		return nil, errors.New("workbuddy: unknown sign-in session")
	}
	wbAuthMu.Lock()
	if s.Status != "pending" {
		out := *s
		wbAuthMu.Unlock()
		return &out, nil
	}
	if time.Now().After(s.Deadline) {
		s.Status = "failed"
		s.Err = "授权超时：5 分钟内未完成登录"
		out := *s
		wbAuthMu.Unlock()
		return &out, nil
	}
	wbAuthMu.Unlock()

	pctx, cancel := context.WithTimeout(ctx, wbAuthPollTimeout)
	defer cancel()
	q := s.State
	data, err := wbCall(pctx, wbAuthClient(), http.MethodGet,
		"/v2/plugin/auth/token?state="+q, nil, nil)
	if err != nil {
		var we *wbError
		if errors.As(err, &we) && (we.Code == wbSignInPendingErr || we.Status == 408 || we.Status == 429) {
			return s, nil // still waiting for the user
		}
		wbAuthMu.Lock()
		s.Status = "failed"
		s.Err = err.Error()
		wbAuthMu.Unlock()
		return s, nil
	}
	if wbStr(data, "accessToken") == "" {
		return s, nil
	}
	cred := &wbCredential{}
	wbMergeToken(cred, data)

	who, err := wbCall(pctx, wbAuthClient(), http.MethodGet,
		"/v2/plugin/login/account?state="+q, map[string]string{
			"Authorization":      "Bearer " + cred.Access,
			"X-Domain":           cred.domain(),
			"X-No-User-Id":       "true",
			"X-No-Enterprise-Id": "true",
		}, nil)
	if err != nil {
		var we *wbError
		if errors.As(err, &we) && we.Code == 12151 {
			return s, nil // account row not bound yet
		}
		wbAuthMu.Lock()
		s.Status = "failed"
		s.Err = err.Error()
		wbAuthMu.Unlock()
		return s, nil
	}
	cred.UID = wbStr(who, "uid")
	if id := wbStr(who, "enterpriseId"); id != "" {
		cred.EnterpriseID = id
	}
	cred.AccountName = wbAccountName(who)
	if cred.Domain == "" {
		cred.Domain = "copilot.tencent.com"
	}

	// The account's callable models come from its product config. A config
	// read failure does not fail the sign-in: the wizard falls back to the
	// channel pool's model list.
	models, merr := wbLiveModels(pctx, wbAuthClient(), cred)
	if merr != nil {
		common.SysError(common.LogText("marketplace: workbuddy model list for seller %d: %s", sellerID, merr.Error()))
	}

	wbAuthMu.Lock()
	s.Status = "authorized"
	s.Credential = cred
	s.Account = cred.AccountName
	s.Models = models
	out := *s
	wbAuthMu.Unlock()
	return &out, nil
}

func wbAccountName(who map[string]any) string {
	person := wbStr(who, "nickname")
	if person == "" {
		person = wbStr(who, "phoneNumber")
	}
	if person == "" {
		person = wbStr(who, "uid")
	}
	entName := wbStr(who, "enterpriseName")
	entID := wbStr(who, "enterpriseId")
	if entID == "" {
		return person
	}
	if entName == "" {
		if len(entID) > 6 {
			entName = "企业账号 …" + entID[len(entID)-6:]
		} else {
			entName = "企业账号"
		}
	}
	if person == "" {
		return entName
	}
	return person + " · " + entName
}

// wbAuthConsume returns the session's credential and forgets the session.
func wbAuthConsume(sellerID int64, id string) (*wbCredential, error) {
	wbAuthMu.Lock()
	defer wbAuthMu.Unlock()
	s, ok := wbAuthSessions[id]
	if !ok || s.SellerID != sellerID || s.Status != "authorized" || s.Credential == nil {
		return nil, errors.New("workbuddy: sign-in session is not authorized")
	}
	delete(wbAuthSessions, id)
	return s.Credential, nil
}

// ---------------------------------------------------------------------------
// Token refresh
// ---------------------------------------------------------------------------

// wbRefreshMu serializes refreshes per listing so a concurrent burst spends
// the refresh token once (the plugin's renewing map; a second spend of the
// same refresh token invalidates it).
var (
	wbRefreshMu     sync.Mutex
	wbRefreshFlying = map[int64]*sync.Mutex{}
)

func wbRefreshLock(listingID int64) *sync.Mutex {
	wbRefreshMu.Lock()
	defer wbRefreshMu.Unlock()
	m, ok := wbRefreshFlying[listingID]
	if !ok {
		m = &sync.Mutex{}
		wbRefreshFlying[listingID] = m
	}
	return m
}

// wbRefreshCredential spends the credential's refresh token once.
func wbRefreshCredential(ctx context.Context, client *http.Client, cred *wbCredential) error {
	headers := map[string]string{
		"X-Refresh-Token":       cred.Refresh,
		"X-Auth-Refresh-Source": "plugin",
		"X-Domain":              cred.domain(),
	}
	for k, v := range wbEnterpriseHeaders(cred) {
		headers[k] = v
	}
	got, err := wbCall(ctx, client, http.MethodPost, "/v2/plugin/auth/token/refresh", headers, map[string]any{})
	if err != nil {
		return err
	}
	if wbStr(got, "accessToken") == "" {
		return errors.New("workbuddy: refresh returned no token")
	}
	wbMergeToken(cred, got)
	return nil
}

// wbPersistCredential writes the (possibly refreshed) bundle back: the access
// token into KeyCiphertext, the OAuth bundle into OAuthCiphertext.
func wbPersistCredential(db *gorm.DB, listingID int64, cred *wbCredential) error {
	keyBlob, err := EncryptListingKey(cred.Access)
	if err != nil {
		return err
	}
	raw, err := common.Marshal(cred)
	if err != nil {
		return err
	}
	oauthBlob, err := EncryptProtectedBlob(raw)
	if err != nil {
		return err
	}
	return db.Model(&Listing{}).Where("id = ?", listingID).Updates(map[string]any{
		"key_ciphertext":   keyBlob,
		"oauth_ciphertext": oauthBlob,
	}).Error
}

// wbCredentialOf decodes the listing's OAuth bundle. nil when the listing is
// not a WorkBuddy OAuth channel.
func wbCredentialOf(listing *Listing) *wbCredential {
	if listing == nil || listing.Vendor != VendorWorkBuddy || len(listing.OAuthCiphertext) == 0 {
		return nil
	}
	raw, err := DecryptProtectedBlob(listing.OAuthCiphertext)
	if err != nil {
		return nil
	}
	var cred wbCredential
	if err := common.Unmarshal([]byte(raw), &cred); err != nil {
		return nil
	}
	return &cred
}

// decryptListingKeyForUse returns the plaintext upstream credential for one
// listing. Ordinary listings decrypt and return; a WorkBuddy OAuth listing
// additionally refreshes the access token first when it is near its end, so
// the caller always gets a usable bearer.
func decryptListingKeyForUse(db *gorm.DB, listing *Listing) (string, error) {
	key, err := DecryptListingKey(listing.KeyCiphertext)
	if err != nil {
		return "", err
	}
	cred := wbCredentialOf(listing)
	if cred == nil {
		return key, nil
	}
	if !cred.stale(time.Now().UnixMilli()) {
		return key, nil
	}
	lock := wbRefreshLock(listing.ID)
	lock.Lock()
	defer lock.Unlock()
	// Re-read under the lock: another request may have just refreshed.
	if err := hydrateListingOAuth(db, listing); err != nil {
		return "", err
	}
	cred = wbCredentialOf(listing)
	if cred == nil {
		return key, nil
	}
	if !cred.stale(time.Now().UnixMilli()) {
		key, err = DecryptListingKey(listing.KeyCiphertext)
		return key, err
	}
	if !cred.refreshable(time.Now().UnixMilli()) {
		return "", errors.New("workbuddy: credential expired; the seller must sign in again")
	}
	client := EgressHTTPClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := wbRefreshCredential(ctx, client, cred); err != nil {
		return "", fmt.Errorf("workbuddy: token refresh: %w", err)
	}
	if err := wbPersistCredential(db, listing.ID, cred); err != nil {
		return "", err
	}
	return cred.Access, nil
}

// hydrateListingOAuth reloads the two encrypted columns of one listing.
func hydrateListingOAuth(db *gorm.DB, listing *Listing) error {
	var row Listing
	if err := db.Select("id", "key_ciphertext", "oauth_ciphertext").
		Where("id = ?", listing.ID).Take(&row).Error; err != nil {
		return err
	}
	listing.KeyCiphertext = row.KeyCiphertext
	listing.OAuthCiphertext = row.OAuthCiphertext
	return nil
}

// ---------------------------------------------------------------------------
// Request fingerprint
// ---------------------------------------------------------------------------

// wbStaticHeaders is the channel header_override written at create time. The
// Authorization header itself comes from the channel key (= access token);
// the rest reproduces the desktop CLI's request fingerprint. {uuid} and
// {uuid:msg} resolve per request (api_request.go).
func wbStaticHeaders(cred *wbCredential) map[string]string {
	h := map[string]string{
		"user-agent":                  wbAgent,
		"x-user-id":                   cred.UID,
		"x-domain":                    cred.domain(),
		"x-product":                   "SaaS",
		"x-ide-type":                  "WorkBuddy",
		"x-ide-name":                  "WorkBuddy",
		"x-ide-version":               wbAppVersion,
		"x-requested-with":            "XMLHttpRequest",
		"x-agent-intent":              "craft",
		"x-agent-purpose":             "conversation",
		"x-agent-type":                "main",
		"x-agent-runtime":             "local-agent",
		"x-stainless-retry-count":     "0",
		"x-stainless-timeout":         "1200",
		"x-stainless-lang":            "js",
		"x-stainless-package-version": wbOpenAISDK,
		"x-stainless-os":              "Windows",
		"x-stainless-arch":            "x64",
		"x-stainless-runtime":         "node",
		"x-stainless-runtime-version": wbNodeVersion,
		"x-conversation-id":           "{uuid}",
		"x-conversation-request-id":   "{uuid}",
		"x-conversation-message-id":   "{uuid}",
		"x-request-id":                "{uuid}",
	}
	for k, v := range wbEnterpriseHeaders(cred) {
		h[strings.ToLower(k)] = v
	}
	return h
}

// wbHeadersForProbe is wbStaticHeaders for code paths that build requests
// themselves (verification, patrol). Dynamic ids get concrete values.
func wbHeadersForProbe(cred *wbCredential) map[string]string {
	h := wbStaticHeaders(cred)
	for k, v := range h {
		if v == "{uuid}" {
			h[k] = wbUUID()
		}
	}
	return h
}

func wbUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ---------------------------------------------------------------------------
// Model probe (streamed; WorkBuddy refuses non-stream chats)
// ---------------------------------------------------------------------------

// wbProbeModelCall is the WorkBuddy variant of probeModelCall: the chat goes
// out streamed, and callability is one content delta plus a completed stream.
func wbProbeModelCall(ctx context.Context, client *http.Client, baseURL, model, apiKey string, headers map[string]string) modelProbeOutcome {
	var out modelProbeOutcome
	reqBody, err := common.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 8,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
	})
	if err != nil {
		out.FailReason = "无法构造模型探活请求"
		return out
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(baseURL, "/")+"/chat/completions", strings.NewReader(string(reqBody)))
	if err != nil {
		out.FailReason = "上游地址格式不正确"
		return out
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	start := time.Now()
	resp, err := client.Do(req)
	out.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		out.FailReason = "模型探活失败：" + stripKey(err.Error(), apiKey)
		return out
	}
	defer resp.Body.Close()
	out.StatusCode = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		out.UpstreamMessage = sanitizeUpstreamMessage(string(raw), apiKey)
		out.FailReason = modelCallFailureReason(resp.StatusCode)
		return out
	}
	// Read the SSE stream: any choices delta proves the model answered.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	body := string(raw)
	gotDelta := strings.Contains(body, `"delta"`) && strings.Contains(body, `"choices"`)
	gotDone := strings.Contains(body, "[DONE]") || strings.Contains(body, `"finish_reason"`)
	if !gotDelta || !gotDone {
		out.FailReason = "模型返回的流式响应不完整"
		return out
	}
	out.OK = true
	return out
}

// listingModelProbe routes the callability probe through the WorkBuddy
// streamed variant when the listing is a WB OAuth channel; every other
// listing keeps the plain OpenAI probe.
func listingModelProbe(ctx context.Context, client *http.Client, listing *Listing, baseURL, model, apiKey string) modelProbeOutcome {
	if cred := wbCredentialOf(listing); cred != nil {
		return wbProbeModelCall(ctx, client, baseURL, model, apiKey, wbHeadersForProbe(cred))
	}
	return probeModelCallOutcome(ctx, client, baseURL, model, apiKey)
}

// wbFingerprintPromptTokens is fingerprintPromptTokens for a WorkBuddy
// candidate: the chat must be streamed, so prompt_tokens is read from the
// usage frame of the SSE stream.
func wbFingerprintPromptTokens(ctx context.Context, client *http.Client, cred *wbCredential, apiKey, upstreamModel, prompt string) (int64, string) {
	body, err := common.Marshal(map[string]any{
		"model":          upstreamModel,
		"messages":       []map[string]string{{"role": "user", "content": prompt}},
		"max_tokens":     1,
		"temperature":    0,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	})
	if err != nil {
		return 0, "request could not be built"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		wbChatBase+"/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return 0, "request could not be built"
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	for name, value := range wbHeadersForProbe(cred) {
		req.Header.Set(name, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "request failed"
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return 0, "request returned " + httpStatusMeaning(resp.StatusCode)
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(line[len("data:"):])
		if payload == "[DONE]" || payload == "" {
			continue
		}
		var chunk struct {
			Usage *struct {
				PromptTokens int64 `json:"prompt_tokens"`
			} `json:"usage"`
		}
		if err := common.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil && chunk.Usage.PromptTokens > 0 {
			return chunk.Usage.PromptTokens, ""
		}
	}
	return 0, "missing prompt token usage"
}

// compareFingerprintForListing is CompareFingerprint with a WorkBuddy-aware
// candidate call. The official side and the caching/verdict logic are
// identical; only the candidate's prompt-token fetch differs (streamed, with
// the account headers).
func compareFingerprintForListing(ctx context.Context, client *http.Client,
	official Vendor, officialKey, officialUpstreamModel string,
	candidate Vendor, listing *Listing, candidateKey, candidateUpstreamModel string,
	prompt string) FingerprintResult {
	cred := wbCredentialOf(listing)
	if cred == nil {
		return CompareFingerprint(ctx, client, official, officialKey, officialUpstreamModel,
			candidate, candidateKey, candidateUpstreamModel, prompt)
	}
	var res FingerprintResult
	if fingerprintHTTPClient != nil {
		client = fingerprintHTTPClient
	}
	if client == nil {
		res.FailReason = "no http client injected for fingerprint comparison"
		return res
	}
	now := fingerprintCacheNow()
	cacheKey := fingerprintCacheKey(official.ID, officialUpstreamModel, prompt)
	officialTokens, cached := fingerprintCacheGet(cacheKey, now)
	if !cached {
		var fail string
		officialTokens, fail = fingerprintPromptTokens(ctx, client, official, officialKey, officialUpstreamModel, prompt)
		if fail != "" {
			res.FailReason = fingerprintFailReason("official", fail)
			return res
		}
		if officialTokens > 0 {
			fingerprintCachePut(cacheKey, officialTokens, now)
		}
	}
	candidateTokens, fail := wbFingerprintPromptTokens(ctx, client, cred, candidateKey, candidateUpstreamModel, prompt)
	if fail != "" {
		res.FailReason = fingerprintFailReason("candidate", fail)
		return res
	}
	res.PromptTokensOfficial = officialTokens
	res.PromptTokensCandidate = candidateTokens
	if officialTokens <= 0 || candidateTokens <= 0 {
		res.FailReason = "missing prompt token usage"
		return res
	}
	if officialTokens < fingerprintMinPromptTokens {
		res.FailReason = "prompt too short to compare"
		return res
	}
	ratio := float64(candidateTokens) / float64(officialTokens)
	diff := officialTokens - candidateTokens
	if diff < 0 {
		diff = -diff
	}
	res.Ratio = math.Round(ratio*10000) / 10000
	res.Checked = true
	res.Comparable = true
	res.WithinTolerance = math.Abs(ratio-1) <= fingerprintTolerance+1e-9 ||
		diff <= fingerprintAbsoluteTolerance
	if !res.WithinTolerance {
		res.FailReason = "prompt token mismatch"
	}
	return res
}

// ---------------------------------------------------------------------------
// Daily check-in
// ---------------------------------------------------------------------------

type wbCheckinResult struct {
	Outcome string // claimed / done / inactive / ineligible / failed
	Credit  int64
	Streak  int64
	Message string
}

// wbCheckin claims the account's daily credit (plugin checkinOf): read the
// activity status, then claim when a checked day is due.
func wbCheckin(ctx context.Context, client *http.Client, cred *wbCredential) wbCheckinResult {
	meter := func(path string) (map[string]any, error) {
		headers := map[string]string{
			"Authorization": "Bearer " + cred.Access,
			"X-User-Id":     cred.UID,
			"X-Domain":      cred.domain(),
			"X-Product":     "SaaS",
			"X-IDE-Type":    "WorkBuddy",
		}
		for k, v := range wbEnterpriseHeaders(cred) {
			headers[k] = v
		}
		return wbCall(ctx, client, http.MethodGet, path, headers, nil)
	}
	status, err := meter("/v2/billing/meter/checkin-activity-status")
	if err != nil {
		if we, ok := err.(*wbError); ok && we.Code == 1001 {
			return wbCheckinResult{Outcome: "done"}
		}
		return wbCheckinResult{Outcome: "failed", Message: err.Error()}
	}
	active, _ := status["active"].(bool)
	if !active {
		return wbCheckinResult{Outcome: "inactive"}
	}
	checked, _ := status["today_checked_in"].(bool)
	if !checked {
		checked, _ = status["todayCheckedIn"].(bool)
	}
	if checked {
		r := wbCheckinResult{Outcome: "done"}
		if c := wbNum(status, "today_credit"); c > 0 {
			r.Credit = c
		} else if c := wbNum(status, "daily_credit"); c > 0 {
			r.Credit = c
		}
		return r
	}
	got, err := meter("/v2/billing/meter/daily-checkin")
	if err != nil {
		if we, ok := err.(*wbError); ok {
			switch we.Code {
			case 1001:
				return wbCheckinResult{Outcome: "done"}
			case 1002:
				return wbCheckinResult{Outcome: "ineligible", Message: we.Msg}
			case 1003:
				return wbCheckinResult{Outcome: "inactive", Message: we.Msg}
			}
			if we.Status == 401 {
				return wbCheckinResult{Outcome: "ineligible", Message: "WorkBuddy 的签到需要 WorkBuddy 自家客户端环境"}
			}
		}
		return wbCheckinResult{Outcome: "failed", Message: err.Error()}
	}
	return wbCheckinResult{
		Outcome: "claimed",
		Credit:  wbNum(got, "credit"),
		Streak:  wbNum(got, "streak_days"),
		Message: wbStr(got, "message"),
	}
}

// wbChinaDay returns the Asia/Shanghai calendar day the check-in activity
// follows (WorkBuddy is a China product; the window resets at CST midnight).
func wbChinaDay(t time.Time) string {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return t.In(loc).Format("2006-01-02")
}

// wbCheckinJob is the scheduled half of daily check-in: for every opted-in
// WorkBuddy channel, claim the daily credit once per China-local day. A
// channel whose account is permanently ineligible is retried weekly, not
// daily, so a hopeless listing does not hammer the endpoint.
func wbCheckinJob(db *gorm.DB, now time.Time) {
	var listings []Listing
	if err := db.Where("vendor = ? AND auto_checkin = ? AND status IN ?",
		VendorWorkBuddy, true,
		[]string{ListingDraft, ListingVerifying, ListingCanary, ListingLive, ListingPaused}).
		Find(&listings).Error; err != nil {
		common.SysError(common.LogText("marketplace: wb-checkin job: %s", err.Error()))
		return
	}
	today := wbChinaDay(now)
	client := EgressVerifyHTTPClient()
	for i := range listings {
		l := &listings[i]
		if l.CheckinAt != nil {
			if wbChinaDay(*l.CheckinAt) == today {
				continue
			}
			if strings.HasPrefix(l.CheckinResult, "ineligible") && now.Sub(*l.CheckinAt) < 7*24*time.Hour {
				continue
			}
		}
		// Small random spacing between channels: a fleet of accounts firing at
		// the same second looks like a bot.
		var jb [1]byte
		_, _ = rand.Read(jb[:])
		time.Sleep(time.Duration(int(jb[0])%45) * time.Second)

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		res := wbRunCheckin(ctx, db, client, l)
		cancel()
		resultText := res.Outcome
		if res.Message != "" {
			resultText = res.Outcome + ": " + res.Message
		}
		finished := time.Now()
		if err := db.Model(&Listing{}).Where("id = ?", l.ID).Updates(map[string]any{
			"checkin_at":     finished,
			"checkin_result": resultText,
		}).Error; err != nil {
			common.SysError(common.LogText("marketplace: wb-checkin job: persist listing %d: %s", l.ID, err.Error()))
			continue
		}
		l.CheckinAt = &finished
		l.CheckinResult = resultText
	}
}

// wbRunCheckin refreshes the listing credential if needed, claims the daily
// credit and records the outcome on the listing row.
func wbRunCheckin(ctx context.Context, db *gorm.DB, client *http.Client, listing *Listing) wbCheckinResult {
	if _, err := decryptListingKeyForUse(db, listing); err != nil {
		return wbCheckinResult{Outcome: "failed", Message: "凭据不可用：" + err.Error()}
	}
	// Re-read the bundle: a refresh inside decryptListingKeyForUse may have
	// rewritten the token.
	cred := wbCredentialOf(listing)
	if cred == nil {
		if err := hydrateListingOAuth(db, listing); err != nil {
			return wbCheckinResult{Outcome: "failed", Message: err.Error()}
		}
		cred = wbCredentialOf(listing)
	}
	if cred == nil {
		return wbCheckinResult{Outcome: "ineligible", Message: "不是 WorkBuddy 授权渠道"}
	}
	return wbCheckin(ctx, client, cred)
}
