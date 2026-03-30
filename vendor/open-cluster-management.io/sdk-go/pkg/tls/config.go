package tls

import (
	"crypto/tls"
	"fmt"
	"strings"

	"k8s.io/klog/v2"
)

const (
	// ConfigMapName is the well-known name of the ConfigMap containing TLS profile settings.
	ConfigMapName = "ocm-tls-profile"

	// ConfigMapKeyMinVersion is the ConfigMap key for the minimum TLS version.
	ConfigMapKeyMinVersion = "minTLSVersion"

	// ConfigMapKeyCipherSuites is the ConfigMap key for cipher suites
	ConfigMapKeyCipherSuites = "cipherSuites"
)

// defaultMinTLSVersion is the fallback when no TLS profile is configured
const defaultMinTLSVersion = "VersionTLS12"

// secureCiphersByName maps IANA names → IDs for ciphers in tls.CipherSuites().
var secureCiphersByName map[string]uint16

// insecureCiphersByName maps IANA names → IDs for ciphers in tls.InsecureCipherSuites().
var insecureCiphersByName map[string]uint16

// cipherNamesByID maps cipher suite IDs → IANA names for all known ciphers.
var cipherNamesByID map[uint16]string

func init() {
	secure := tls.CipherSuites()
	insecure := tls.InsecureCipherSuites()

	secureCiphersByName = make(map[string]uint16, len(secure))
	for _, s := range secure {
		secureCiphersByName[s.Name] = s.ID
	}

	insecureCiphersByName = make(map[string]uint16, len(insecure))
	for _, s := range insecure {
		insecureCiphersByName[s.Name] = s.ID
	}

	cipherNamesByID = make(map[uint16]string, len(secure)+len(insecure))
	for _, s := range secure {
		cipherNamesByID[s.ID] = s.Name
	}
	for _, s := range insecure {
		cipherNamesByID[s.ID] = s.Name
	}
}

// TLSConfig represents parsed TLS configuration
type TLSConfig struct {
	MinVersion   uint16
	CipherSuites []uint16
}

// parseTLSVersion converts a TLS version string to the corresponding crypto/tls constant
func parseTLSVersion(version string) (uint16, error) {
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

// parseCipherSuites converts IANA cipher suite names to Go crypto/tls constants.
// Secure ciphers (tls.CipherSuites) are accepted silently. Insecure ciphers
// (tls.InsecureCipherSuites) are accepted but logged as a warning.
// Returns a list of cipher suite IDs and a list of unrecognized cipher names.
func parseCipherSuites(cipherString string) ([]uint16, []string) {
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

		if id, ok := secureCiphersByName[name]; ok {
			cipherSuites = append(cipherSuites, id)
			continue
		}
		if id, ok := insecureCiphersByName[name]; ok {
			klog.Warningf("Cipher suite %s is insecure and should not be used in production", name)
			cipherSuites = append(cipherSuites, id)
			continue
		}
		unsupported = append(unsupported, name)
	}

	return cipherSuites, unsupported
}

// GetDefaultTLSConfig returns a TLS config with safe defaults (TLS 1.2)
func GetDefaultTLSConfig() *TLSConfig {
	return &TLSConfig{
		MinVersion:   tls.VersionTLS12,
		CipherSuites: nil, // Use Go's default cipher suites for TLS 1.2
	}
}

// ConfigFromFlags creates TLS config from command-line flags
func ConfigFromFlags(minVersion, cipherSuites string) (*TLSConfig, error) {
	minVersion = strings.TrimSpace(minVersion)
	cipherSuites = strings.TrimSpace(cipherSuites)
	if minVersion == "" && cipherSuites == "" {
		return nil, nil // No flags provided
	}

	cfg := &TLSConfig{}

	// Parse min version
	if minVersion != "" {
		ver, err := parseTLSVersion(minVersion)
		if err != nil {
			return nil, fmt.Errorf("invalid --tls-min-version: %w", err)
		}
		cfg.MinVersion = ver
	} else {
		cfg.MinVersion = tls.VersionTLS12
	}

	// Parse cipher suites
	if cipherSuites != "" {
		suites, unsupported := parseCipherSuites(cipherSuites)
		if len(unsupported) > 0 {
			return nil, fmt.Errorf("unsupported cipher suites: %v", unsupported)
		}
		cfg.CipherSuites = suites
	}

	return cfg, nil
}

// VersionToString converts a TLS version constant to its string representation
func VersionToString(version uint16) string {
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

// CipherSuitesToString converts cipher suite IDs back to IANA names
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

// ConfigToFunc returns a function that applies the TLS configuration to a tls.Config.
// It is suitable for use with controller-runtime's TLSOpts (webhook/metrics servers).
// If tlsCfg is nil (e.g. returned by ConfigFromFlags when no flags are set), the
// returned function is a no-op that leaves the tls.Config unchanged.
func ConfigToFunc(tlsCfg *TLSConfig) func(*tls.Config) {
	if tlsCfg == nil {
		return func(*tls.Config) {}
	}
	return func(config *tls.Config) {
		config.MinVersion = tlsCfg.MinVersion
		if tlsCfg.MinVersion == tls.VersionTLS13 {
			config.MaxVersion = tls.VersionTLS13
		} else if len(tlsCfg.CipherSuites) > 0 {
			config.CipherSuites = tlsCfg.CipherSuites
		}
	}
}

// cipherIDToName converts a cipher suite ID to its IANA name.
func cipherIDToName(id uint16) string {
	return cipherNamesByID[id]
}
