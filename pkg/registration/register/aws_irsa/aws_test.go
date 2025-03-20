package aws_irsa

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/cache"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	managedclusterv1lister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"

	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/test/integration/util"
)

func TestIsCRApproved(t *testing.T) {
	cases := []struct {
		name       string
		cr         string
		crApproved bool
	}{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			indexer := cache.NewIndexer(
				cache.MetaNamespaceKeyFunc,
				cache.Indexers{
					cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
				})
			require.NoError(t, indexer.Add(c.cr))
			lister := managedclusterv1lister.NewManagedClusterLister(indexer)
			ctrl := &v1AWSIRSAControl{
				hubManagedClusterLister: lister,
			}
			crApproved, err := ctrl.isApproved(c.name)
			assert.NoError(t, err)
			if crApproved != c.crApproved {
				t.Errorf("expected %t, but got %t", c.crApproved, crApproved)
			}
		})
	}
}

func TestBuildKubeconfig(t *testing.T) {
	cases := []struct {
		name           string
		server         string
		proxyURL       string
		caData         []byte
		clientCertFile string
		clientKeyFile  string
		AuthInfoExec   *clientcmdapi.ExecConfig
	}{
		{
			name:   "without proxy",
			server: "https://127.0.0.1:6443",
			AuthInfoExec: &clientcmdapi.ExecConfig{
				APIVersion: "client.authentication.k8s.io/v1beta1",
				Command:    "/awscli/dist/aws",
				Args: []string{
					"--region",
					"us-west-2",
					"eks",
					"get-token",
					"--cluster-name",
					"hub-cluster1",
					"--output",
					"json",
					"--role",
					fmt.Sprintf("arn:aws:iam::123456789012:role/ocm-hub-%s", ManagedClusterIAMRoleSuffix),
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bootstrapKubeconfig := &clientcmdapi.Config{
				Clusters: map[string]*clientcmdapi.Cluster{
					"default-cluster": {
						Server:                c.server,
						InsecureSkipTLSVerify: false,
					}},
				// Define a context that connects the auth info and cluster, and set it as the default
				Contexts: map[string]*clientcmdapi.Context{register.DefaultKubeConfigContext: {
					Cluster:   "default-cluster",
					AuthInfo:  register.DefaultKubeConfigAuth,
					Namespace: "configuration",
				}},
				CurrentContext: register.DefaultKubeConfigContext,
				AuthInfos: map[string]*clientcmdapi.AuthInfo{
					register.DefaultKubeConfigAuth: {
						ClientCertificate: c.clientCertFile,
						ClientKey:         c.clientKeyFile,
					},
				},
			}

			registerImpl := &AWSIRSADriver{}
			registerImpl.hubClusterArn = util.HubClusterArn
			registerImpl.managedClusterRoleSuffix = ManagedClusterIAMRoleSuffix
			kubeconfig := registerImpl.BuildKubeConfigFromTemplate(bootstrapKubeconfig)
			currentContext, ok := kubeconfig.Contexts[kubeconfig.CurrentContext]
			if !ok {
				t.Errorf("current context %q not found: %v", kubeconfig.CurrentContext, kubeconfig)
			}

			cluster, ok := kubeconfig.Clusters[currentContext.Cluster]
			if !ok {
				t.Errorf("cluster %q not found: %v", currentContext.Cluster, kubeconfig)
			}

			if cluster.Server != c.server {
				t.Errorf("expected server %q, but got %q", c.server, cluster.Server)
			}

			authInfo, ok := kubeconfig.AuthInfos[currentContext.AuthInfo]
			if !ok {
				t.Errorf("auth info %q not found: %v", currentContext.AuthInfo, kubeconfig)
			}

			if authInfo.Exec.APIVersion != c.AuthInfoExec.APIVersion {
				t.Errorf("The value of api version is %s but is expected to be %s", authInfo.Exec.APIVersion, c.AuthInfoExec.APIVersion)
			}

			if authInfo.Exec.Command != c.AuthInfoExec.Command {
				t.Errorf("Value of AuthInfo.Exec.Command is expected to be %s but got %s", authInfo.Exec.Command, c.AuthInfoExec.Command)
			}

			if !reflect.DeepEqual(authInfo.Exec.Args, c.AuthInfoExec.Args) {
				t.Errorf("Value of AuthInfo.Exec.Args is expected to be %s but got  %s", authInfo.Exec.Args, c.AuthInfoExec.Args)
			}

		})
	}
}
