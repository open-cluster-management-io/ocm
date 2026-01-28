package token

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	// RefreshThreshold is the percentage of token lifetime remaining before refresh
	// When the token has 20% or less of its lifetime remaining, it will be refreshed
	RefreshThreshold = 0.2
)

// jwtClaims represents the claims section of a JWT token
type jwtClaims struct {
	Exp        int64              `json:"exp"` // Expiration time (Unix timestamp)
	Iat        int64              `json:"iat"` // Issued at time (Unix timestamp)
	Kubernetes kubernetesMetadata `json:"kubernetes.io,omitempty"`
}

// kubernetesMetadata represents Kubernetes-specific claims in the token
type kubernetesMetadata struct {
	ServiceAccount serviceAccountMetadata `json:"serviceaccount,omitempty"`
}

// serviceAccountMetadata represents service account information in the token
type serviceAccountMetadata struct {
	UID string `json:"uid,omitempty"`
}

// parseToken parses a JWT token and extracts issue time, expiration time, and service account UID
func parseToken(tokenData []byte) (issueTime, expirationTime time.Time, uid string, err error) {
	token := string(tokenData)

	// JWT tokens have three parts separated by dots: header.payload.signature
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		err = fmt.Errorf("invalid JWT token format: expected 3 parts, got %d", len(parts))
		return
	}

	// Decode the payload (second part)
	payloadEncoded := parts[1]
	// JWT uses base64url encoding, which may need padding
	if l := len(payloadEncoded) % 4; l > 0 {
		payloadEncoded += strings.Repeat("=", 4-l)
	}

	payloadBytes, decodeErr := base64.URLEncoding.DecodeString(payloadEncoded)
	if decodeErr != nil {
		err = fmt.Errorf("failed to decode JWT payload: %w", decodeErr)
		return
	}

	// Parse the claims
	var claims jwtClaims
	if unmarshalErr := json.Unmarshal(payloadBytes, &claims); unmarshalErr != nil {
		err = fmt.Errorf("failed to unmarshal JWT claims: %w", unmarshalErr)
		return
	}

	if claims.Exp == 0 {
		err = fmt.Errorf("token does not have expiration claim")
		return
	}

	if claims.Iat == 0 {
		err = fmt.Errorf("token does not have issued-at claim")
		return
	}

	issueTime = time.Unix(claims.Iat, 0)
	expirationTime = time.Unix(claims.Exp, 0)
	uid = claims.Kubernetes.ServiceAccount.UID

	return
}

// isTokenValid checks if a token is still valid based on its expiration and RefreshThreshold (20%)
// Returns (valid, reason) where reason describes why the token is invalid
// desiredUID is the expected service account UID; if empty, UID check is skipped
func isTokenValid(tokenData []byte, desiredUID string) (bool, string) {
	if len(tokenData) == 0 {
		return false, "token is empty"
	}

	issueTime, expirationTime, uid, err := parseToken(tokenData)
	if err != nil {
		return false, fmt.Sprintf("failed to parse token: %v", err)
	}

	// Check if the token's service account UID matches the desired UID
	if desiredUID != "" && uid != desiredUID {
		return false, fmt.Sprintf("token service account UID mismatch (expected %s, got %s)", desiredUID, uid)
	}

	now := time.Now()
	if expirationTime.Before(now) {
		return false, fmt.Sprintf("token has expired at %s", expirationTime.Format(time.RFC3339))
	}

	totalLifetime := expirationTime.Sub(issueTime)
	// Guard against non-positive lifetime (exp == iat or exp < iat)
	if totalLifetime <= 0 {
		return false, "token has non-positive lifetime"
	}

	remaining := time.Until(expirationTime)

	// Calculate the percentage of lifetime remaining
	remainingPercentage := remaining.Seconds() / totalLifetime.Seconds()

	// Token is valid if it has more than the threshold percentage of its lifetime remaining
	if remainingPercentage <= RefreshThreshold {
		return false, fmt.Sprintf("token is close to expiration (%.1f%% remaining, threshold %.1f%%)",
			remainingPercentage*100, RefreshThreshold*100)
	}

	return true, ""
}
