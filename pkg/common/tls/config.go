package tls

import (
	"crypto/tls"
	"fmt"
	"strings"
)

const (
	// DefaultMinTLSVersion is the fallback when no TLS profile is configured
	DefaultMinTLSVersion = "VersionTLS12"

	// ConfigMapName is the name of the ConfigMap containing TLS profile settings
	ConfigMapName = "ocm-tls-profile"

	// ConfigMapKeyMinVersion is the ConfigMap key for minimum TLS version
	ConfigMapKeyMinVersion = "minTLSVersion"

	// ConfigMapKeyCipherSuites is the ConfigMap key for cipher suites
	ConfigMapKeyCipherSuites = "cipherSuites"
)

// TLSConfig represents parsed TLS configuration
type TLSConfig struct {
	MinVersion   uint16
	CipherSuites []uint16
}

// ParseTLSVersion converts a TLS version string to the corresponding crypto/tls constant
func ParseTLSVersion(version string) (uint16, error) {
	version = strings.TrimSpace(version)
	switch version {
	case "VersionTLS10", "TLSv1.0":
		return tls.VersionTLS10, nil
	case "VersionTLS11", "TLSv1.1":
		return tls.VersionTLS11, nil
	case "VersionTLS12", "TLSv1.2", "":
		// Empty string defaults to TLS 1.2
		return tls.VersionTLS12, nil
	case "VersionTLS13", "TLSv1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unknown TLS version: %s", version)
	}
}

// ParseCipherSuites converts OpenSSL-style cipher names to Go crypto/tls constants
// Returns a list of cipher suite IDs and a list of unsupported cipher names
func ParseCipherSuites(cipherString string) ([]uint16, []string) {
	if strings.TrimSpace(cipherString) == "" {
		return nil, nil
	}

	cipherNames := strings.Split(cipherString, ",")
	cipherSuites := make([]uint16, 0, len(cipherNames))
	unsupported := make([]string, 0)

	for _, name := range cipherNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		if suite, ok := cipherMap[name]; ok {
			cipherSuites = append(cipherSuites, suite)
		} else {
			unsupported = append(unsupported, name)
		}
	}

	return cipherSuites, unsupported
}

// BuildTLSConfig creates a crypto/tls.Config from parsed TLS configuration
func BuildTLSConfig(tlsCfg *TLSConfig) *tls.Config {
	config := &tls.Config{
		MinVersion: tlsCfg.MinVersion,
	}

	// TLS 1.3 has fixed cipher suites, don't set CipherSuites field
	if tlsCfg.MinVersion == tls.VersionTLS13 {
		config.MaxVersion = tls.VersionTLS13
	} else if len(tlsCfg.CipherSuites) > 0 {
		config.CipherSuites = tlsCfg.CipherSuites
	}

	return config
}

// GetDefaultTLSConfig returns a TLS config with safe defaults (TLS 1.2)
func GetDefaultTLSConfig() *TLSConfig {
	return &TLSConfig{
		MinVersion:   tls.VersionTLS12,
		CipherSuites: nil, // Use Go's default cipher suites for TLS 1.2
	}
}

// TLSConfigFromFlags creates TLS config from command-line flags
func TLSConfigFromFlags(minVersion, cipherSuites string) (*TLSConfig, error) {
	if minVersion == "" && cipherSuites == "" {
		return nil, nil // No flags provided
	}

	cfg := &TLSConfig{}

	// Parse min version
	if minVersion != "" {
		ver, err := ParseTLSVersion(minVersion)
		if err != nil {
			return nil, fmt.Errorf("invalid --tls-min-version: %w", err)
		}
		cfg.MinVersion = ver
	} else {
		cfg.MinVersion = tls.VersionTLS12
	}

	// Parse cipher suites
	if cipherSuites != "" {
		suites, unsupported := ParseCipherSuites(cipherSuites)
		if len(unsupported) > 0 {
			return nil, fmt.Errorf("unsupported cipher suites: %v", unsupported)
		}
		cfg.CipherSuites = suites
	}

	return cfg, nil
}

// TLSVersionToString converts a TLS version constant to its string representation
func TLSVersionToString(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "VersionTLS10"
	case tls.VersionTLS11:
		return "VersionTLS11"
	case tls.VersionTLS12:
		return "VersionTLS12"
	case tls.VersionTLS13:
		return "VersionTLS13"
	default:
		return fmt.Sprintf("Unknown (0x%04x)", version)
	}
}

// CipherSuitesToString converts cipher suite IDs back to OpenSSL-style names
func CipherSuitesToString(suites []uint16) string {
	if len(suites) == 0 {
		return ""
	}

	names := make([]string, 0, len(suites))
	for _, suite := range suites {
		name := cipherIDToName(suite)
		if name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, ",")
}

// cipherIDToName converts a cipher suite ID to its OpenSSL-style name
func cipherIDToName(id uint16) string {
	for name, suiteID := range cipherMap {
		if suiteID == id {
			return name
		}
	}
	return ""
}
