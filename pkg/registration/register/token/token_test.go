package token

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonfake "open-cluster-management.io/api/client/addon/clientset/versioned/fake"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/ocm/pkg/registration/register"
)

// newTestAddonClients creates fake addon clients for testing
func newTestAddonClients() *register.AddOnClients {
	addonClient := addonfake.NewSimpleClientset()
	addonInformerFactory := addoninformers.NewSharedInformerFactory(addonClient, 10*time.Minute)
	return &register.AddOnClients{
		AddonClient:   addonClient,
		AddonInformer: addonInformerFactory.Addon().V1alpha1().ManagedClusterAddOns(),
	}
}

func TestTokenDriver_BuildKubeConfigFromTemplate(t *testing.T) {
	addonClients := newTestAddonClients()
	driver := NewTokenDriverForAddOn("test-addon", "test-cluster", NewTokenOption(), nil, addonClients)

	template := &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"hub": {
				Server: "https://hub.example.com",
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			register.DefaultKubeConfigContext: {
				Cluster:  "hub",
				AuthInfo: register.DefaultKubeConfigAuth,
			},
		},
		CurrentContext: register.DefaultKubeConfigContext,
	}

	result := driver.BuildKubeConfigFromTemplate(template)

	if result.AuthInfos == nil {
		t.Fatal("AuthInfos should not be nil")
	}

	authInfo, ok := result.AuthInfos[register.DefaultKubeConfigAuth]
	if !ok {
		t.Fatal("AuthInfo not found")
	}

	if authInfo.TokenFile != TokenFile {
		t.Errorf("Expected TokenFile to be %q, got %q", TokenFile, authInfo.TokenFile)
	}
}

func TestTokenDriver_ManagedClusterDecorator(t *testing.T) {
	addonClients := newTestAddonClients()
	driver := NewTokenDriverForAddOn("test-addon", "test-cluster", NewTokenOption(), nil, addonClients)

	cluster := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
	}

	result := driver.ManagedClusterDecorator(cluster)

	if result != cluster {
		t.Error("ManagedClusterDecorator should return the same cluster object")
	}
}

func TestTokenDriver_InformerHandler(t *testing.T) {
	addonClients := newTestAddonClients()
	driver := NewTokenDriverForAddOn("test-addon", "test-cluster", NewTokenOption(), nil, addonClients)

	informer, filter := driver.InformerHandler()

	if informer == nil {
		t.Error("Expected informer to be non-nil when addonClients is provided")
	}

	if filter == nil {
		t.Error("Expected filter to be non-nil when addonClients is provided")
	}
}

func TestTokenDriver_Process(t *testing.T) {
	t.Skip("Skipping Process test - requires full mock implementation")
	// This test requires mocking:
	// - AddonInformer with lister
	// - AddonClient for status updates
	// - TokenControl for token creation
	// Consider implementing with fake clients in future
}

func TestShouldRefreshToken(t *testing.T) {
	tests := []struct {
		name          string
		tokenAge      time.Duration
		tokenExpiry   time.Duration
		shouldRefresh bool
	}{
		{
			name:          "fresh token - no refresh",
			tokenAge:      0,
			tokenExpiry:   1 * time.Hour,
			shouldRefresh: false,
		},
		{
			name:          "token near expiry - refresh needed",
			tokenAge:      50 * time.Minute,
			tokenExpiry:   1 * time.Hour,
			shouldRefresh: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt := NewTokenOption()
			addonClients := newTestAddonClients()

			driver := NewTokenDriverForAddOn("test-addon", "test-cluster", opt, nil, addonClients)

			now := time.Now()
			iat := now.Add(-tt.tokenAge).Unix()
			exp := now.Add(tt.tokenExpiry - tt.tokenAge).Unix()
			mockToken := createMockJWT(t, exp, iat)

			secret := &corev1.Secret{
				Data: map[string][]byte{
					TokenFile: []byte(mockToken),
				},
			}

			shouldRefresh, err := driver.shouldRefreshToken(context.Background(), secret, "")
			if err != nil {
				t.Fatalf("shouldRefreshToken failed: %v", err)
			}

			if shouldRefresh != tt.shouldRefresh {
				t.Errorf("Expected shouldRefresh=%v, got %v", tt.shouldRefresh, shouldRefresh)
			}
		})
	}
}

func TestShouldRefreshToken_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		secretData    map[string][]byte
		desiredUID    string
		shouldRefresh bool
		wantError     bool
	}{
		{
			name:          "empty secret data - refresh needed",
			secretData:    map[string][]byte{},
			shouldRefresh: true,
			wantError:     false,
		},
		{
			name: "empty token - refresh needed",
			secretData: map[string][]byte{
				TokenFile: []byte(""),
			},
			shouldRefresh: true,
			wantError:     false,
		},
		{
			name: "invalid token format - refresh needed",
			secretData: map[string][]byte{
				TokenFile: []byte("invalid.token"),
			},
			shouldRefresh: true,
			wantError:     false,
		},
		{
			name: "UID mismatch - refresh needed",
			secretData: map[string][]byte{
				TokenFile: []byte(createMockJWTWithUID(t, time.Now().Add(1*time.Hour).Unix(), time.Now().Unix(), "wrong-uid")),
			},
			desiredUID:    "expected-uid",
			shouldRefresh: true,
			wantError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addonClients := newTestAddonClients()
			driver := NewTokenDriverForAddOn("test-addon", "test-cluster", NewTokenOption(), nil, addonClients)

			secret := &corev1.Secret{
				Data: tt.secretData,
			}

			shouldRefresh, err := driver.shouldRefreshToken(context.Background(), secret, tt.desiredUID)
			if (err != nil) != tt.wantError {
				t.Errorf("shouldRefreshToken() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if shouldRefresh != tt.shouldRefresh {
				t.Errorf("shouldRefreshToken() = %v, want %v", shouldRefresh, tt.shouldRefresh)
			}
		})
	}
}

func TestParseServiceAccountUIDFromMessage(t *testing.T) {
	tests := []struct {
		name      string
		message   string
		wantUID   string
		wantError bool
	}{
		{
			name:      "valid message",
			message:   "ServiceAccount cluster1/cluster1-addon1-agent (UID: 12345678-1234-1234-1234-123456789abc) is ready",
			wantUID:   "12345678-1234-1234-1234-123456789abc",
			wantError: false,
		},
		{
			name:      "missing UID prefix",
			message:   "ServiceAccount cluster1/cluster1-addon1-agent is ready",
			wantUID:   "",
			wantError: true,
		},
		{
			name:      "missing closing parenthesis",
			message:   "ServiceAccount cluster1/cluster1-addon1-agent (UID: 12345678-1234-1234-1234-123456789abc is ready",
			wantUID:   "",
			wantError: true,
		},
		{
			name:      "empty UID",
			message:   "ServiceAccount cluster1/cluster1-addon1-agent (UID: ) is ready",
			wantUID:   "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addonClients := newTestAddonClients()
			driver := NewTokenDriverForAddOn("test-addon", "test-cluster", NewTokenOption(), nil, addonClients)

			uid, err := driver.parseServiceAccountUIDFromMessage(tt.message)
			if (err != nil) != tt.wantError {
				t.Errorf("parseServiceAccountUIDFromMessage() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if uid != tt.wantUID {
				t.Errorf("parseServiceAccountUIDFromMessage() = %v, want %v", uid, tt.wantUID)
			}
		})
	}
}

func TestIsHubKubeConfigValid(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	now := time.Now()
	validToken := createMockJWTWithUID(t, now.Add(1*time.Hour).Unix(), now.Unix(), "test-uid")
	expiredToken := createMockJWTWithUID(t, now.Add(-1*time.Hour).Unix(), now.Add(-2*time.Hour).Unix(), "test-uid")

	tests := []struct {
		name      string
		setupFunc func() string
		wantValid bool
		wantError bool
	}{
		{
			name: "valid token file",
			setupFunc: func() string {
				dir := filepath.Join(tmpDir, "valid")
				os.MkdirAll(dir, 0755)
				os.WriteFile(filepath.Join(dir, TokenFile), []byte(validToken), 0644)
				return dir
			},
			wantValid: true,
			wantError: false,
		},
		{
			name: "token file does not exist",
			setupFunc: func() string {
				return filepath.Join(tmpDir, "nonexistent")
			},
			wantValid: false,
			wantError: false,
		},
		{
			name: "expired token",
			setupFunc: func() string {
				dir := filepath.Join(tmpDir, "expired")
				os.MkdirAll(dir, 0755)
				os.WriteFile(filepath.Join(dir, TokenFile), []byte(expiredToken), 0644)
				return dir
			},
			wantValid: false,
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addonClients := newTestAddonClients()
			driver := NewTokenDriverForAddOn("test-addon", "test-cluster", NewTokenOption(), nil, addonClients)
			hubKubeconfigDir := tt.setupFunc()

			secretOption := register.SecretOption{
				HubKubeconfigDir: hubKubeconfigDir,
			}

			valid, err := driver.IsHubKubeConfigValid(context.Background(), secretOption)
			if (err != nil) != tt.wantError {
				t.Errorf("IsHubKubeConfigValid() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if valid != tt.wantValid {
				t.Errorf("IsHubKubeConfigValid() = %v, want %v", valid, tt.wantValid)
			}
		})
	}
}

func TestParseToken(t *testing.T) {
	now := time.Now()
	iat := now.Unix()
	exp := now.Add(1 * time.Hour).Unix()
	expectedUID := "test-uid-12345"

	tests := []struct {
		name      string
		token     string
		wantIat   int64
		wantExp   int64
		wantUID   string
		wantError bool
	}{
		{
			name:      "valid token",
			token:     createMockJWTWithUID(t, exp, iat, expectedUID),
			wantIat:   iat,
			wantExp:   exp,
			wantUID:   expectedUID,
			wantError: false,
		},
		{
			name:      "invalid format - only 2 parts",
			token:     "header.payload",
			wantError: true,
		},
		{
			name:      "invalid format - 4 parts",
			token:     "header.payload.signature.extra",
			wantError: true,
		},
		{
			name:      "invalid base64 payload",
			token:     "header.!!!invalid!!!.signature",
			wantError: true,
		},
		{
			name:      "missing exp claim",
			token:     createMockJWTWithoutClaim(t, "exp", iat),
			wantError: true,
		},
		{
			name:      "missing iat claim",
			token:     createMockJWTWithoutClaim(t, "iat", exp),
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issueTime, expirationTime, uid, err := parseToken([]byte(tt.token))
			if (err != nil) != tt.wantError {
				t.Errorf("parseToken() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if !tt.wantError {
				if issueTime.Unix() != tt.wantIat {
					t.Errorf("parseToken() issueTime = %v, want %v", issueTime.Unix(), tt.wantIat)
				}
				if expirationTime.Unix() != tt.wantExp {
					t.Errorf("parseToken() expirationTime = %v, want %v", expirationTime.Unix(), tt.wantExp)
				}
				if uid != tt.wantUID {
					t.Errorf("parseToken() uid = %v, want %v", uid, tt.wantUID)
				}
			}
		})
	}
}

func TestIsTokenValid(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		tokenData  []byte
		desiredUID string
		wantValid  bool
		reason     string
	}{
		{
			name:      "empty token",
			tokenData: []byte(""),
			wantValid: false,
			reason:    "token is empty",
		},
		{
			name:      "valid token with plenty of life remaining",
			tokenData: []byte(createMockJWTWithUID(t, now.Add(1*time.Hour).Unix(), now.Unix(), "test-uid")),
			wantValid: true,
		},
		{
			name:      "token near expiration (within refresh threshold)",
			tokenData: []byte(createMockJWTWithUID(t, now.Add(10*time.Minute).Unix(), now.Add(-50*time.Minute).Unix(), "test-uid")),
			wantValid: false,
		},
		{
			name:      "expired token",
			tokenData: []byte(createMockJWTWithUID(t, now.Add(-1*time.Hour).Unix(), now.Add(-2*time.Hour).Unix(), "test-uid")),
			wantValid: false,
		},
		{
			name:       "UID mismatch",
			tokenData:  []byte(createMockJWTWithUID(t, now.Add(1*time.Hour).Unix(), now.Unix(), "wrong-uid")),
			desiredUID: "expected-uid",
			wantValid:  false,
		},
		{
			name:      "non-positive lifetime (exp == iat)",
			tokenData: []byte(createMockJWTWithUID(t, now.Unix(), now.Unix(), "test-uid")),
			wantValid: false,
		},
		{
			name:      "non-positive lifetime (exp < iat)",
			tokenData: []byte(createMockJWTWithUID(t, now.Add(-1*time.Hour).Unix(), now.Unix(), "test-uid")),
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid, reason := isTokenValid(tt.tokenData, tt.desiredUID)
			if valid != tt.wantValid {
				t.Errorf("isTokenValid() = %v, want %v (reason: %s)", valid, tt.wantValid, reason)
			}
		})
	}
}

// createMockJWT creates a simplified mock JWT token for testing
// Note: This is not a real signed JWT, just enough structure for parsing tests
func createMockJWT(t *testing.T, exp, iat int64) string {
	t.Helper()
	return createMockJWTWithUID(t, exp, iat, "12345678-1234-1234-1234-123456789abc")
}

// createMockJWTWithUID creates a mock JWT token with a specific UID
func createMockJWTWithUID(t *testing.T, exp, iat int64, uid string) string {
	t.Helper()

	// JWT header (base64url encoded)
	header := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9" // {"alg":"RS256","typ":"JWT"}

	// Create payload with exp, iat, and kubernetes metadata
	payload := map[string]interface{}{
		"exp": exp,
		"iat": iat,
		"sub": "system:serviceaccount:default:test-sa",
		"aud": "https://kubernetes.default.svc",
		"kubernetes.io": map[string]interface{}{
			"namespace": "default",
			"serviceaccount": map[string]interface{}{
				"name": "test-sa",
				"uid":  uid,
			},
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	payloadEncoded := base64.RawURLEncoding.EncodeToString(payloadBytes)

	// Mock signature (base64url encoded)
	signature := "mock-signature"

	return header + "." + payloadEncoded + "." + signature
}

// createMockJWTWithoutClaim creates a mock JWT token missing a specific claim
func createMockJWTWithoutClaim(t *testing.T, missingClaim string, value int64) string {
	t.Helper()

	header := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9"

	payload := map[string]interface{}{
		"sub": "system:serviceaccount:default:test-sa",
		"aud": "https://kubernetes.default.svc",
		"kubernetes.io": map[string]interface{}{
			"namespace": "default",
			"serviceaccount": map[string]interface{}{
				"name": "test-sa",
				"uid":  "test-uid",
			},
		},
	}

	// Add the claims except the one we want to omit
	if missingClaim != "exp" {
		payload["exp"] = value
	}
	if missingClaim != "iat" {
		payload["iat"] = value
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	payloadEncoded := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signature := "mock-signature"

	return header + "." + payloadEncoded + "." + signature
}

// TestTokenDriver_NilGuards tests nil pointer guards for critical components
func TestTokenDriver_NilGuards(t *testing.T) {
	tests := []struct {
		name             string
		setupDriver      func() *TokenDriver
		testFunc         func(t *testing.T, driver *TokenDriver)
		expectError      bool
		expectedErrorMsg string
	}{
		{
			name: "createToken with nil tokenControl",
			setupDriver: func() *TokenDriver {
				addonClients := newTestAddonClients()
				return NewTokenDriverForAddOn("test-addon", "test-cluster", NewTokenOption(), nil, addonClients)
			},
			testFunc: func(t *testing.T, driver *TokenDriver) {
				_, _, err := driver.createToken(context.Background())
				if err == nil {
					t.Error("Expected error when tokenControl is nil")
				}
				if err != nil && err.Error() != "token control not initialized" {
					t.Errorf("Expected 'token control not initialized' error, got: %v", err)
				}
			},
			expectError:      true,
			expectedErrorMsg: "token control not initialized",
		},
		{
			name: "InformerHandler returns correct informer and filter",
			setupDriver: func() *TokenDriver {
				addonClients := newTestAddonClients()
				return NewTokenDriverForAddOn("test-addon", "test-cluster", NewTokenOption(), nil, addonClients)
			},
			testFunc: func(t *testing.T, driver *TokenDriver) {
				informer, filter := driver.InformerHandler()
				if informer == nil {
					t.Error("Expected non-nil informer")
				}
				if filter == nil {
					t.Error("Expected non-nil filter")
				}
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driver := tt.setupDriver()
			tt.testFunc(t, driver)
		})
	}
}

// TestTokenDriver_EnsureTokenInfrastructureReady tests infrastructure readiness checks
func TestTokenDriver_EnsureTokenInfrastructureReady(t *testing.T) {
	tests := []struct {
		name          string
		addon         *addonv1alpha1.ManagedClusterAddOn
		expectedUID   string
		expectedReady bool
		expectedError bool
	}{
		{
			name: "no TokenInfrastructureReady condition",
			addon: &addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-addon",
				},
				Status: addonv1alpha1.ManagedClusterAddOnStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectedUID:   "",
			expectedReady: false,
			expectedError: false,
		},
		{
			name: "TokenInfrastructureReady condition is False",
			addon: &addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-addon",
				},
				Status: addonv1alpha1.ManagedClusterAddOnStatus{
					Conditions: []metav1.Condition{
						{
							Type:   "TokenInfrastructureReady",
							Status: metav1.ConditionFalse,
							Reason: "ServiceAccountNotFound",
						},
					},
				},
			},
			expectedUID:   "",
			expectedReady: false,
			expectedError: false,
		},
		{
			name: "TokenInfrastructureReady is True with valid UID",
			addon: &addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-addon",
				},
				Status: addonv1alpha1.ManagedClusterAddOnStatus{
					Conditions: []metav1.Condition{
						{
							Type:    "TokenInfrastructureReady",
							Status:  metav1.ConditionTrue,
							Reason:  "ServiceAccountReady",
							Message: "ServiceAccount default/test-addon-agent (UID: 12345678-1234-1234-1234-123456789abc) is ready",
						},
					},
				},
			},
			expectedUID:   "12345678-1234-1234-1234-123456789abc",
			expectedReady: true,
			expectedError: false,
		},
		{
			name: "TokenInfrastructureReady is True but invalid message format",
			addon: &addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-addon",
				},
				Status: addonv1alpha1.ManagedClusterAddOnStatus{
					Conditions: []metav1.Condition{
						{
							Type:    "TokenInfrastructureReady",
							Status:  metav1.ConditionTrue,
							Reason:  "ServiceAccountReady",
							Message: "Invalid message without UID",
						},
					},
				},
			},
			expectedUID:   "",
			expectedReady: false,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addonClients := newTestAddonClients()
			driver := NewTokenDriverForAddOn("test-addon", "test-cluster", NewTokenOption(), nil, addonClients)

			uid, ready, err := driver.ensureTokenInfrastructureReady(context.Background(), tt.addon)

			if (err != nil) != tt.expectedError {
				t.Errorf("Expected error=%v, got error=%v", tt.expectedError, err)
			}
			if ready != tt.expectedReady {
				t.Errorf("Expected ready=%v, got ready=%v", tt.expectedReady, ready)
			}
			if uid != tt.expectedUID {
				t.Errorf("Expected UID=%q, got UID=%q", tt.expectedUID, uid)
			}
		})
	}
}

// TestTokenDriver_ParseServiceAccountUIDFromMessage tests UID parsing from condition messages
func TestTokenDriver_ParseServiceAccountUIDFromMessage(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		expectedUID string
		wantError   bool
	}{
		{
			name:        "valid message with UID",
			message:     "ServiceAccount default/test-addon-agent (UID: 12345678-1234-1234-1234-123456789abc) is ready",
			expectedUID: "12345678-1234-1234-1234-123456789abc",
			wantError:   false,
		},
		{
			name:        "message without UID prefix",
			message:     "ServiceAccount is ready",
			expectedUID: "",
			wantError:   true,
		},
		{
			name:        "message with UID prefix but no closing paren",
			message:     "ServiceAccount (UID: 12345678-1234-1234-1234-123456789abc",
			expectedUID: "",
			wantError:   true,
		},
		{
			name:        "empty message",
			message:     "",
			expectedUID: "",
			wantError:   true,
		},
		{
			name:        "message with empty UID",
			message:     "ServiceAccount (UID: ) is ready",
			expectedUID: "",
			wantError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addonClients := newTestAddonClients()
			driver := NewTokenDriverForAddOn("test-addon", "test-cluster", NewTokenOption(), nil, addonClients)

			uid, err := driver.parseServiceAccountUIDFromMessage(tt.message)

			if (err != nil) != tt.wantError {
				t.Errorf("Expected error=%v, got error=%v", tt.wantError, err)
			}
			if uid != tt.expectedUID {
				t.Errorf("Expected UID=%q, got UID=%q", tt.expectedUID, uid)
			}
		})
	}
}

// TestTokenDriver_InformerHandlerFilter tests the event filter function
func TestTokenDriver_InformerHandlerFilter(t *testing.T) {
	addonClients := newTestAddonClients()
	driver := NewTokenDriverForAddOn("test-addon", "test-cluster", NewTokenOption(), nil, addonClients)

	_, filter := driver.InformerHandler()
	if filter == nil {
		t.Fatal("Expected non-nil filter")
	}

	tests := []struct {
		name           string
		obj            interface{}
		expectedResult bool
	}{
		{
			name: "matching addon name",
			obj: &addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-addon",
				},
			},
			expectedResult: true,
		},
		{
			name: "non-matching addon name",
			obj: &addonv1alpha1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{
					Name: "other-addon",
				},
			},
			expectedResult: false,
		},
		{
			name:           "invalid object type",
			obj:            "not-a-valid-object",
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter(tt.obj)
			if result != tt.expectedResult {
				t.Errorf("Expected filter result=%v, got=%v", tt.expectedResult, result)
			}
		})
	}
}
