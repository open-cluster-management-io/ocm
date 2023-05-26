package clientcert

const (
	// KubeconfigFile is the name of the kubeconfig file in kubeconfigSecret
	KubeconfigFile = "kubeconfig"
	// TLSKeyFile is the name of tls key file in kubeconfigSecret
	TLSKeyFile = "tls.key"
	// TLSCertFile is the name of the tls cert file in kubeconfigSecret
	TLSCertFile = "tls.crt"

	// ClusterCertificateRotatedCondition is a condition type that client certificate is rotated
	ClusterCertificateRotatedCondition = "ClusterCertificateRotated"
)
