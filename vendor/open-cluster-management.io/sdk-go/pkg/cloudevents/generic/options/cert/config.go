package cert

import (
	"encoding/base64"
	"fmt"
	"os"
)

type Bytes []byte

func (b *Bytes) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return err
	}
	*b = Bytes(decoded)
	return nil
}

func (b Bytes) MarshalYAML() (interface{}, error) {
	return base64.StdEncoding.EncodeToString(b), nil
}

// CertConfig includes the certificates configuration for building TLS connection
type CertConfig struct {
	// CAFile is the file path to a cert file for the gRPC server certificate authority.
	CAFile string `json:"caFile,omitempty" yaml:"caFile,omitempty"`
	// CAData contains PEM-encoded certificate authority certificates with base64 encoded. Overrides CAFile
	CAData Bytes `json:"caData,omitempty" yaml:"caData,omitempty"`

	// ClientCertFile is the file path to a client cert file for TLS.
	ClientCertFile string `json:"clientCertFile,omitempty" yaml:"clientCertFile,omitempty"`
	// ClientCertData contains PEM-encoded client certificates with base64 encoded. Overrides ClientCertFile
	ClientCertData Bytes `json:"clientCertData,omitempty" yaml:"clientCertData,omitempty"`

	// ClientKeyFile is the file path to a client key file for TLS.
	ClientKeyFile string `json:"clientKeyFile,omitempty" yaml:"clientKeyFile,omitempty"`
	// ClientKeyData contains PEM-encoded client key with base64 encoded. Overrides ClientKeyFile
	ClientKeyData Bytes `json:"clientKeyData,omitempty" yaml:"clientKeyData,omitempty"`
}

func (c *CertConfig) EmbedCerts() error {
	if c == nil {
		return fmt.Errorf("CertConfig is nil")
	}

	if len(c.CAData) == 0 && c.CAFile != "" {
		ca, err := os.ReadFile(c.CAFile)
		if err != nil {
			return err
		}
		c.CAData = ca
	}

	if len(c.ClientCertData) == 0 && c.ClientCertFile != "" {
		cert, err := os.ReadFile(c.ClientCertFile)
		if err != nil {
			return err
		}
		c.ClientCertData = cert
	}

	if len(c.ClientKeyData) == 0 && c.ClientKeyFile != "" {
		key, err := os.ReadFile(c.ClientKeyFile)
		if err != nil {
			return err
		}
		c.ClientKeyData = key
	}

	return nil
}

func (c *CertConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("CertConfig is nil")
	}

	if (len(c.ClientCertData) == 0 && len(c.ClientKeyData) != 0) ||
		(len(c.ClientCertData) != 0 && len(c.ClientKeyData) == 0) {
		return fmt.Errorf("either both or none of clientCertFile and clientKeyFile must be set")
	}

	return nil
}

func (c *CertConfig) HasCerts() bool {
	if c == nil {
		return false
	}

	return len(c.ClientCertData) != 0 && len(c.ClientKeyData) != 0
}
