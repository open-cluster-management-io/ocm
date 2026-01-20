package csr

import (
	"crypto/x509/pkix"
	"errors"
	"fmt"

	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	"open-cluster-management.io/ocm/pkg/registration/register"
)

// CSROption includes options that is used to create and monitor csrs
type CSROption struct {
	// ObjectMeta is the ObjectMeta shared by all created csrs. It should use GenerateName instead of Name
	// to generate random csr names
	ObjectMeta metav1.ObjectMeta
	// Subject represents the subject of the client certificate used to create csrs
	Subject *pkix.Name
	// DNSNames represents DNS names used to create the client certificate
	DNSNames []string
	// SignerName is the name of the signer specified in the created csrs
	SignerName string

	// EventFilterFunc matches csrs created with above options
	EventFilterFunc factory.EventFilterFunc
}

// Option is the option set from flag
type Option struct {
	// ExpirationSeconds is the requested duration of validity of the issued
	// certificate.
	// Certificate signers may not honor this field for various reasons:
	//
	//   1. Old signer that is unaware of the field (such as the in-tree
	//      implementations prior to v1.22)
	//   2. Signer whose configured maximum is shorter than the requested duration
	//   3. Signer whose configured minimum is longer than the requested duration
	//
	// The minimum valid value for expirationSeconds is 3600, i.e. 1 hour.
	ExpirationSeconds int32
}

// Ensure Option implements register.CSRConfiguration interface at compile time
var _ register.CSRConfiguration = &Option{}

func NewCSROption() *Option {
	return &Option{}
}

func (o *Option) AddFlags(fs *pflag.FlagSet) {
	fs.Int32Var(&o.ExpirationSeconds, "client-cert-expiration-seconds", o.ExpirationSeconds,
		"The requested duration in seconds of validity of the issued client certificate. If this is not set, "+
			"the value of --cluster-signing-duration command-line flag of the kube-controller-manager will be used.")
}

func (o *Option) Validate() error {
	if o.ExpirationSeconds != 0 && o.ExpirationSeconds < 3600 {
		return errors.New("client certificate expiration seconds must greater or qual to 3600")
	}
	return nil
}

func (o *Option) GetExpirationSeconds() int32 {
	return o.ExpirationSeconds
}

func haltCSRCreationFunc(indexer cache.Indexer, clusterName string) func() bool {
	return func() bool {
		items, err := indexer.ByIndex(indexByCluster, clusterName)
		if err != nil {
			return false
		}

		if len(items) >= clusterCSRThreshold {
			return true
		}
		return false
	}
}

func haltAddonCSRCreationFunc(indexer cache.Indexer, clusterName, addonName string) func() bool {
	return func() bool {
		items, err := indexer.ByIndex(indexByAddon, fmt.Sprintf("%s/%s", clusterName, addonName))
		if err != nil {
			return false
		}

		if len(items) >= addonCSRThreshold {
			return true
		}

		return false
	}
}
