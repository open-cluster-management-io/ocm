package helpers

const (
	AwsIrsaAuthType = "awsirsa"
	CSRAuthType     = "csr"
	GRPCCAuthType   = "grpc"
)

const GRPCCAuthSigner = "open-cluster-management.io/grpc"

// CSRUserAnnotation will be added to a CSR and used to identify the user who request the CSR
const CSRUserAnnotation = "open-cluster-management.io/csruser"
