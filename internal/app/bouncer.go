package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"time"

	"sidus.io/charge/internal/util"
)

const (
	allowedInfoPath                = "/.well-known/charge-allowed"
	defaultAllowedCacheDuration    = 30 * time.Minute
	defaultNotAllowedCacheDuration = 5 * time.Minute
)

// Bouncer checks if a domain allows connection from the chargé deployment
// by fetching the allowed deployment URLs from the domain's /.well-known/charge-allowed endpoint.
// It uses caching and singleflight to minimize abuse of the endpoint and improve performance.
type Bouncer struct {
	sfGroup       *util.SingleFlightGroup[BounceStatus]
	m             *util.SyncMap[string, BounceStatus]
	deploymentURL string
	allowInsecure bool
}

type BounceStatus struct {
	validBefore time.Time
	allowed     bool
	reason      string
}

func (s *BounceStatus) Allowed() error {
	if s.allowed {
		return nil
	}
	return NotAllowedError{
		Reason:           s.reason,
		MayTryAgainAfter: s.validBefore,
	}
}

type NotAllowedError struct {
	Reason           string
	MayTryAgainAfter time.Time
}

func (e NotAllowedError) Error() string {
	return fmt.Sprintf("not allowed: %s (may try again after %s)", e.Reason, e.MayTryAgainAfter.Format(time.RFC3339))
}

func (b *Bouncer) Allowed(callbackUrl *url.URL) error {
	domain := callbackUrl.Hostname()
	if status, ok := b.m.Load(domain); ok && time.Now().Before(status.validBefore) {
		return status.Allowed()
	}

	status, err, _ := b.sfGroup.Do(domain, func() (BounceStatus, error) {
		// Double-check cache inside singleflight
		if st, ok := b.m.Load(domain); ok && time.Now().Before(st.validBefore) {
			return st, nil
		}

		newStatus := b.fetchStatus(callbackUrl)
		b.m.Store(domain, newStatus)
		return newStatus, nil
	})
	if err != nil {
		return fmt.Errorf("singleflight: %w", err)
	}

	return status.Allowed()
}

func (b *Bouncer) fetchStatus(callbackUrl *url.URL) BounceStatus {
	status := BounceStatus{
		validBefore: time.Now().Add(defaultNotAllowedCacheDuration),
	}

	if !b.allowInsecure && callbackUrl.Scheme != "https" {
		status.reason = "insecure scheme"
		return status
	}

	allowedURL := fmt.Sprintf("%s://%s/%s", callbackUrl.Scheme, callbackUrl.Host, allowedInfoPath)
	req, err := http.NewRequest(http.MethodGet, allowedURL, nil)
	if err != nil {
		status.reason = fmt.Sprintf("new request: %s", err)
		return status
	}

	req.Header.Set("User-Agent", "charge/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		status.reason = fmt.Sprintf("request error: %s", err)
		return status
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		status.reason = fmt.Sprintf("non-200 status code: %d", resp.StatusCode)
		return status
	}

	var result struct {
		AllowedDeploymentURLs []string `json:"allowedDeploymentUrls"`
		CacheDurationSeconds  int      `json:"cacheDurationSeconds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		status.reason = fmt.Sprintf("invalid response body: %s", err)
		return status
	}

	status.allowed = slices.Contains(result.AllowedDeploymentURLs, b.deploymentURL)
	if status.allowed {
		status.validBefore = time.Now().Add(defaultAllowedCacheDuration)
	} else {
		status.reason = "deployment URL not allowed"
	}

	if result.CacheDurationSeconds > 0 {
		status.validBefore = time.Now().Add(time.Duration(result.CacheDurationSeconds) * time.Second)
	}

	return status
}
