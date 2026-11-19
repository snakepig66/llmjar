package marketplace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func testWBCredential() *wbCredential {
	return &wbCredential{
		Access:      "wb-access-aaa",
		Refresh:     "wb-refresh-rrr",
		Expires:     time.Now().Add(time.Hour).UnixMilli(),
		Domain:      "copilot.tencent.com",
		UID:         "uid-12345",
		AccountName: "测试账号",
	}
}

// The dedup/ban identity is the account, not the token: rotating access and
// refresh tokens must leave the fingerprint untouched.
func TestWBIdentityFingerprintStableAcrossRefresh(t *testing.T) {
	preserveAnonSecret(t)
	SetAnonSecret("test-secret")
	cred := testWBCredential()
	fp1, err := wbIdentityFingerprint(cred)
	require.NoError(t, err)

	cred.Access = "wb-access-rotated"
	cred.Refresh = "wb-refresh-rotated"
	cred.Expires = time.Now().Add(2 * time.Hour).UnixMilli()
	fp2, err := wbIdentityFingerprint(cred)
	require.NoError(t, err)
	assert.Equal(t, fp1, fp2)

	// A different account is a different credential.
	other := testWBCredential()
	other.UID = "uid-99999"
	fp3, err := wbIdentityFingerprint(other)
	require.NoError(t, err)
	assert.NotEqual(t, fp1, fp3)
}

// Sign-in sessions are one-time and bound to their seller.
func TestWBAuthConsumeOwnershipAndOneTime(t *testing.T) {
	wbAuthMu.Lock()
	wbAuthSessions = map[string]*wbAuthSession{}
	wbAuthMu.Unlock()

	cred := testWBCredential()
	s := &wbAuthSession{
		ID:         "sess-1",
		SellerID:   7,
		Status:     "authorized",
		Credential: cred,
	}
	wbAuthMu.Lock()
	wbAuthSessions[s.ID] = s
	wbAuthMu.Unlock()

	// Another seller cannot consume it.
	_, err := wbAuthConsume(8, "sess-1")
	require.Error(t, err)
	wbAuthMu.Lock()
	_, stillThere := wbAuthSessions["sess-1"]
	wbAuthMu.Unlock()
	assert.True(t, stillThere, "a failed consume must not drop the session")

	// A pending session cannot be consumed.
	wbAuthMu.Lock()
	wbAuthSessions["sess-1"].Status = "pending"
	wbAuthMu.Unlock()
	_, err = wbAuthConsume(7, "sess-1")
	require.Error(t, err)

	wbAuthMu.Lock()
	wbAuthSessions["sess-1"].Status = "authorized"
	wbAuthMu.Unlock()
	got, err := wbAuthConsume(7, "sess-1")
	require.NoError(t, err)
	assert.Equal(t, cred.Access, got.Access)

	// One-time: a second consume fails.
	_, err = wbAuthConsume(7, "sess-1")
	require.Error(t, err)
}

// A WorkBuddy listing stores the OAuth bundle encrypted, pins the channel to
// the dedicated adaptor, and carries the desktop header fingerprint with
// {uuid} placeholders.
func TestCreateListingWorkBuddy(t *testing.T) {
	db := newListingTestDB(t)
	cred := testWBCredential()
	raw, err := common.Marshal(cred)
	require.NoError(t, err)

	listing, err := CreateListing(db, CreateListingInput{
		SellerID:          10024,
		VendorID:          VendorWorkBuddy,
		APIKey:            cred.Access,
		OAuthBundle:       string(raw),
		Models:            []string{"deepseek-flash"},
		UpstreamModels:    map[string]string{"deepseek-flash": "kimi-k2"},
		MultiplierMilli:   500,
		PlanTermsAccepted: true,
		AutoCheckin:       true,
	})
	require.NoError(t, err)

	fp, err := wbIdentityFingerprint(cred)
	require.NoError(t, err)
	assert.Equal(t, fp, listing.KeyFingerprint)
	require.NotEmpty(t, listing.OAuthCiphertext)
	assert.NotContains(t, string(listing.OAuthCiphertext), cred.Refresh)
	assert.True(t, listing.AutoCheckin)

	back := wbCredentialOf(&listing)
	require.NotNil(t, back)
	assert.Equal(t, cred.Refresh, back.Refresh, "the refresh token round-trips through the blob")
	assert.Equal(t, cred.UID, back.UID)

	var ch model.Channel
	require.NoError(t, db.First(&ch, listing.ChannelID).Error)
	assert.Equal(t, constant.ChannelTypeWorkBuddy, ch.Type)
	require.NotNil(t, ch.BaseURL)
	assert.Equal(t, wbChatBase, *ch.BaseURL)
	require.NotNil(t, ch.HeaderOverride)
	headers := gjson.Parse(*ch.HeaderOverride)
	assert.Equal(t, cred.UID, headers.Get("x-user-id").String())
	assert.True(t, strings.HasPrefix(headers.Get("user-agent").String(), "WorkBuddy/"))
	assert.Equal(t, "{uuid}", headers.Get("x-request-id").String())

	// Non-WB listings never get the check-in flag.
	other, err := CreateListing(db, CreateListingInput{
		SellerID:        10024,
		VendorID:        "deepseek",
		APIKey:          listingTestKey,
		Models:          []string{"deepseek-flash"},
		MultiplierMilli: 300,
		AutoCheckin:     true, // ignored outside WorkBuddy
	})
	require.NoError(t, err)
	assert.False(t, other.AutoCheckin)
}

// A listing whose OAuth bundle cannot be decoded is rejected.
func TestCreateListingWorkBuddyRejectsBadBundle(t *testing.T) {
	db := newListingTestDB(t)
	_, err := CreateListing(db, CreateListingInput{
		SellerID:          10024,
		VendorID:          VendorWorkBuddy,
		APIKey:            "whatever",
		OAuthBundle:       `{not json`,
		Models:            []string{"deepseek-flash"},
		MultiplierMilli:   500,
		PlanTermsAccepted: true,
	})
	require.Error(t, err)

	// A bundle whose access token disagrees with api_key is rejected.
	cred := testWBCredential()
	raw, _ := common.Marshal(cred)
	_, err = CreateListing(db, CreateListingInput{
		SellerID:          10024,
		VendorID:          VendorWorkBuddy,
		APIKey:            "different-token",
		OAuthBundle:       string(raw),
		Models:            []string{"deepseek-flash"},
		MultiplierMilli:   500,
		PlanTermsAccepted: true,
	})
	require.Error(t, err)
}

// The check-in claim maps WorkBuddy's plugin-API codes to stable outcomes.
func TestWBCheckinOutcomes(t *testing.T) {
	mux := http.NewServeMux()
	var status, claim map[string]any
	mux.HandleFunc("/v2/billing/meter/checkin-activity-status", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := common.Marshal(map[string]any{"code": 0, "data": status})
		_, _ = w.Write(raw)
	})
	mux.HandleFunc("/v2/billing/meter/daily-checkin", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := common.Marshal(claim)
		_, _ = w.Write(raw)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	old := wbPluginEndpoint
	wbPluginEndpoint = srv.URL
	defer func() { wbPluginEndpoint = old }()

	cred := testWBCredential()
	ctx := context.Background()

	// Claim succeeds.
	status = map[string]any{"active": true, "today_checked_in": false}
	claim = map[string]any{"code": 0, "data": map[string]any{"credit": 50, "streak_days": 3}}
	res := wbCheckin(ctx, srv.Client(), cred)
	assert.Equal(t, "claimed", res.Outcome)
	assert.Equal(t, int64(50), res.Credit)
	assert.Equal(t, int64(3), res.Streak)

	// Already checked in today → done, with the daily credit surfaced.
	status = map[string]any{"active": true, "today_checked_in": true, "today_credit": 30}
	res = wbCheckin(ctx, srv.Client(), cred)
	assert.Equal(t, "done", res.Outcome)
	assert.Equal(t, int64(30), res.Credit)

	// Inactive activity → inactive without calling the claim endpoint.
	status = map[string]any{"active": false}
	res = wbCheckin(ctx, srv.Client(), cred)
	assert.Equal(t, "inactive", res.Outcome)

	// Plugin code 1002 → ineligible.
	status = map[string]any{"active": true, "today_checked_in": false}
	claim = map[string]any{"code": 1002, "msg": "not eligible"}
	res = wbCheckin(ctx, srv.Client(), cred)
	assert.Equal(t, "ineligible", res.Outcome)

	// Plugin code 1001 on the claim → done.
	claim = map[string]any{"code": 1001, "msg": "already checked"}
	res = wbCheckin(ctx, srv.Client(), cred)
	assert.Equal(t, "done", res.Outcome)
}

// WorkBuddy buyers get stream=true forced upstream; the body also switches to
// the classic max_tokens dialect.
func TestRewritePassthroughJSONForceUpstreamStream(t *testing.T) {
	out, err := rewritePassthroughJSON([]byte(
		`{"model":"buyer-model","stream":false,"max_completion_tokens":16,"messages":[]}`),
		passthroughRewrite{
			UpstreamModel:       "wb-upstream",
			Stream:              true,
			ForceUpstreamStream: true,
			RelayFormat:         types.RelayFormatOpenAI,
		})
	require.NoError(t, err)
	got := gjson.ParseBytes(out)
	assert.True(t, got.Get("stream").Bool())
	assert.True(t, got.Get("stream_options.include_usage").Bool())
	assert.Equal(t, int64(16), got.Get("max_tokens").Int())
	assert.False(t, got.Get("max_completion_tokens").Exists())
	assert.Equal(t, "wb-upstream", got.Get("model").String())
}

// The sign-in flow drives state → token → account through the plugin API.
func TestWBAuthFlow(t *testing.T) {
	mux := http.NewServeMux()
	var tokenCalls int
	mux.HandleFunc("/v2/plugin/auth/state", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := common.Marshal(map[string]any{
			"code": 0,
			"data": map[string]any{"state": "st-1", "authUrl": "https://login.tencent.example/page"},
		})
		_, _ = w.Write(raw)
	})
	mux.HandleFunc("/v2/plugin/auth/token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		var payload map[string]any
		if r.Method == http.MethodGet {
			if tokenCalls == 1 {
				payload = map[string]any{"code": wbSignInPendingErr, "msg": "pending"}
			} else {
				payload = map[string]any{"code": 0, "data": map[string]any{
					"accessToken": "wb-access-aaa", "refreshToken": "wb-refresh-rrr",
					"expiresIn": 3600, "domain": "copilot.tencent.com",
				}}
			}
		}
		raw, _ := common.Marshal(payload)
		_, _ = w.Write(raw)
	})
	mux.HandleFunc("/v2/plugin/login/account", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := common.Marshal(map[string]any{"code": 0, "data": map[string]any{
			"uid": "uid-12345", "nickname": "测试账号",
		}})
		_, _ = w.Write(raw)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	old := wbPluginEndpoint
	wbPluginEndpoint = srv.URL
	defer func() { wbPluginEndpoint = old }()
	oldClient := wbHTTPClient
	wbHTTPClient = srv.Client()
	defer func() { wbHTTPClient = oldClient }()

	wbAuthMu.Lock()
	wbAuthSessions = map[string]*wbAuthSession{}
	wbAuthMu.Unlock()

	ctx := context.Background()
	s, err := wbAuthStart(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, "pending", s.Status)
	assert.Contains(t, s.AuthURL, "https://login.tencent.example/page")
	assert.Contains(t, s.AuthURL, "version=")

	// First poll: still pending.
	p, err := wbAuthPoll(ctx, 7, s.ID)
	require.NoError(t, err)
	assert.Equal(t, "pending", p.Status)

	// Second poll completes: token + account fetched.
	p, err = wbAuthPoll(ctx, 7, s.ID)
	require.NoError(t, err)
	assert.Equal(t, "authorized", p.Status)
	require.NotNil(t, p.Credential)
	assert.Equal(t, "wb-access-aaa", p.Credential.Access)
	assert.Equal(t, "uid-12345", p.Credential.UID)

	// Another seller cannot even see the session.
	_, err = wbAuthPoll(ctx, 8, s.ID)
	require.Error(t, err)
}
