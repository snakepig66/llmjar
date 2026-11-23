package marketplace

// api_seller_wb.go — seller-facing WorkBuddy OAuth sign-in and daily
// check-in endpoints (workbuddy.go).

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
)

// postSellerWBAuthStart begins a WorkBuddy browser sign-in: the response
// carries the Tencent sign-in page the seller opens; the session expires with
// WorkBuddy's own 5-minute poll window.
func postSellerWBAuthStart(c *gin.Context) {
	sellerID, ok := sellerUserID(c)
	if !ok {
		return
	}
	s, err := wbAuthStart(c.Request.Context(), sellerID)
	if err != nil {
		common.SysError(common.LogText("marketplace: workbuddy auth start for seller %d: %s", sellerID, err.Error()))
		common.ApiErrorStatus(c, http.StatusBadGateway, errors.New("无法发起 WorkBuddy 授权，请稍后重试"))
		return
	}
	common.ApiSuccess(c, gin.H{
		"session_id": s.ID,
		"auth_url":   s.AuthURL,
		"expires_in": int(wbAuthSessionTTL.Seconds()),
	})
}

// getSellerWBAuthStatus performs one server-side token poll per call. The
// frontend polls this endpoint while the seller completes the Tencent page.
func getSellerWBAuthStatus(c *gin.Context) {
	sellerID, ok := sellerUserID(c)
	if !ok {
		return
	}
	id := strings.TrimSpace(c.Query("session_id"))
	if id == "" {
		common.ApiErrorStatus(c, http.StatusBadRequest, errors.New("session_id is required"))
		return
	}
	s, err := wbAuthPoll(c.Request.Context(), sellerID, id)
	if err != nil {
		common.ApiErrorStatus(c, http.StatusNotFound, errors.New("授权会话不存在或已过期"))
		return
	}
	out := gin.H{"status": s.Status}
	if s.Status == "authorized" {
		out["account_name"] = s.Account
		out["models"] = s.Models
		// The account's model ids become the catalogue's upstream names: each
		// carries a suggested standard-model match (exact → date-stripped →
		// path-stripped, matchPoolModelName), and the priced pool is the whole
		// sellable set — the same payload POST /seller/listings/probe returns.
		if db := sellerDB(c); db != nil {
			ids := make([]string, 0, len(s.Models))
			for _, m := range s.Models {
				ids = append(ids, m.ID)
			}
			now := time.Now()
			catalog := sellerModelCatalogFor(db, ids, now)
			out["upstream_models"] = catalog.UpstreamModels
			out["pool_models"] = catalog.PoolModels
			out["coefficients"] = poolCoefficients(db, VendorWorkBuddy)
			verified := make([]string, 0, len(catalog.UpstreamModels))
			for _, um := range catalog.UpstreamModels {
				if um.Priced && um.SuggestedModel != nil {
					verified = append(verified, *um.SuggestedModel)
				}
			}
			out["verified_models"] = verified
		}
	}
	if s.Status == "failed" {
		out["error"] = s.Err
	}
	common.ApiSuccess(c, out)
}

// postSellerListingCheckin manually runs the WorkBuddy daily check-in for one
// seller-owned channel, mainly so a seller can see the outcome without
// waiting for the nightly job.
func postSellerListingCheckin(c *gin.Context) {
	sellerID, ok := sellerUserID(c)
	if !ok {
		return
	}
	db := sellerDB(c)
	if db == nil {
		return
	}
	id, ok := sellerIDParam(c)
	if !ok {
		return
	}
	listing, ok := requireOwnListing(c, db, id, sellerID)
	if !ok {
		return
	}
	if listing.Vendor != VendorWorkBuddy || len(listing.OAuthCiphertext) == 0 {
		common.ApiErrorStatus(c, http.StatusBadRequest, errors.New("该渠道不是 WorkBuddy 授权渠道"))
		return
	}
	res := wbRunCheckin(c.Request.Context(), db, EgressVerifyHTTPClient(), &listing)
	now := time.Now()
	resultText := res.Outcome
	if res.Message != "" {
		resultText = res.Outcome + ": " + res.Message
	}
	_ = db.Model(&Listing{}).Where("id = ?", listing.ID).Updates(map[string]any{
		"checkin_at":     now,
		"checkin_result": resultText,
	}).Error
	common.ApiSuccess(c, gin.H{
		"outcome": res.Outcome,
		"credit":  res.Credit,
		"streak":  res.Streak,
		"message": res.Message,
	})
}
