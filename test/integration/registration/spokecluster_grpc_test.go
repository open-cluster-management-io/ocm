package registration_test

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"
	addonce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon/v1alpha1"
	clusterce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	csrce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/csr"
	eventce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	leasece "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/lease"
	pbv1 "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protobuf/v1"
	cloudeventsgrpc "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc"
	cemetrics "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/metrics"
	sdkgrpc "open-cluster-management.io/sdk-go/pkg/server/grpc"
	grpcauthn "open-cluster-management.io/sdk-go/pkg/server/grpc/authn"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/hub"
	"open-cluster-management.io/ocm/pkg/registration/register/factory"
	regsgrpc "open-cluster-management.io/ocm/pkg/registration/register/grpc"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/pkg/server/services/addon"
	"open-cluster-management.io/ocm/pkg/server/services/cluster"
	"open-cluster-management.io/ocm/pkg/server/services/csr"
	"open-cluster-management.io/ocm/pkg/server/services/event"
	"open-cluster-management.io/ocm/pkg/server/services/lease"
	"open-cluster-management.io/ocm/test/integration/util"
)

const grpcTest = "grpctest"

var _ = ginkgo.Describe("Registration using GRPC", ginkgo.Ordered, ginkgo.Label("grpc-test"), func() {
	var err error

	var postfix string

	var stopGRPCServer context.CancelFunc
	var stopGRPCAgent context.CancelFunc
	var stopKubeAgent context.CancelFunc

	var gRPCServerOptions *sdkgrpc.GRPCServerOptions
	var gRPCCAKeyFile string

	var bootstrapGRPCConfigFile string

	var hubOptionWithGRPC *hub.HubManagerOptions
	var hubKubeConfigSecret string
	var hubKubeConfigDir string
	var hubGRPCConfigSecret string
	var hubGRPCConfigDir string

	var grpcManagedClusterName string
	var kubeManagedClusterName string

	ginkgo.BeforeAll(func() {
		// stop the hub and start grpc server
		stopHub()

		tempDir, err := os.MkdirTemp("", fmt.Sprintf("registration-%s", grpcTest))
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(tempDir).ToNot(gomega.BeEmpty())

		bootstrapGRPCConfigFile = path.Join(tempDir, "grpcconfig")
		_, gRPCServerOptions, gRPCCAKeyFile, err = util.CreateGRPCConfigs(bootstrapGRPCConfigFile, "8090")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		var grpcServerCtx context.Context
		grpcServerCtx, stopGRPCServer = context.WithCancel(context.Background())
		hook, err := util.NewGRPCServerRegistrationHook(hubCfg)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		grpcEventServer := cloudeventsgrpc.NewGRPCBroker(cloudeventsgrpc.NewBrokerOptions())
		grpcEventServer.RegisterService(grpcServerCtx, clusterce.ManagedClusterEventDataType,
			cluster.NewClusterService(hook.ClusterClient, hook.ClusterInformers.Cluster().V1().ManagedClusters()))
		grpcEventServer.RegisterService(grpcServerCtx, csrce.CSREventDataType,
			csr.NewCSRService(hook.KubeClient, hook.KubeInformers.Certificates().V1().CertificateSigningRequests()))
		grpcEventServer.RegisterService(grpcServerCtx, addonce.ManagedClusterAddOnEventDataType,
			addon.NewAddonService(hook.AddOnClient, hook.AddOnInformers.Addon().V1alpha1().ManagedClusterAddOns()))
		grpcEventServer.RegisterService(grpcServerCtx, eventce.EventEventDataType,
			event.NewEventService(hook.KubeClient))
		grpcEventServer.RegisterService(grpcServerCtx, leasece.LeaseEventDataType,
			lease.NewLeaseService(hook.KubeClient, hook.KubeInformers.Coordination().V1().Leases()))

		go func() {
			defer ginkgo.GinkgoRecover()
			hook.Run(grpcServerCtx)
		}()

		authorizer := util.NewMockAuthorizer()
		server := sdkgrpc.NewGRPCServer(gRPCServerOptions).
			WithAuthenticator(grpcauthn.NewMtlsAuthenticator()).
			WithUnaryAuthorizer(authorizer).
			WithStreamAuthorizer(authorizer).
			WithRegisterFunc(func(s *grpc.Server) {
				pbv1.RegisterCloudEventServiceServer(s, grpcEventServer)
			}).
			WithExtraMetrics(cemetrics.CloudEventsGRPCMetrics()...)

		go func() {
			err := server.Run(grpcServerCtx)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()
	})

	ginkgo.AfterAll(func() {
		stopGRPCServer()
		// restart hub with default option
		startHub(hubOption)
	})

	ginkgo.Context("ManagedCluster registration", func() {
		ginkgo.BeforeEach(func() {
			postfix = rand.String(5)
			hubOptionWithGRPC = hub.NewHubManagerOptions()
			hubOptionWithGRPC.EnabledRegistrationDrivers = []string{operatorv1.CSRAuthType, operatorv1.GRPCAuthType}
			hubOptionWithGRPC.GRPCCAFile = gRPCServerOptions.ClientCAFile
			hubOptionWithGRPC.GRPCCAKeyFile = gRPCCAKeyFile
			startHub(hubOptionWithGRPC)

			grpcManagedClusterName = fmt.Sprintf("%s-managedcluster-grpc-%s", grpcTest, postfix)
			kubeManagedClusterName = fmt.Sprintf("%s-managedcluster-kube-%s", grpcTest, postfix)

			hubGRPCConfigSecret = fmt.Sprintf("%s-hub-grpcconfig-secret-%s", grpcTest, postfix)
			hubGRPCConfigDir = path.Join(util.TestDir, fmt.Sprintf("%s-grpc-%s", grpcTest, postfix), "hub-kubeconfig")
			grpcDriverOption := factory.NewOptions()
			grpcDriverOption.RegistrationAuth = operatorv1.GRPCAuthType
			grpcDriverOption.GRPCOption = &regsgrpc.Option{
				BootstrapConfigFile: bootstrapGRPCConfigFile,
				ConfigFile:          path.Join(hubGRPCConfigDir, "config.yaml"),
			}
			grpcAgentOptions := &spoke.SpokeAgentOptions{
				BootstrapKubeconfig:      bootstrapKubeConfigFile,
				HubKubeconfigSecret:      hubGRPCConfigSecret,
				ClusterHealthCheckPeriod: 1 * time.Minute,
				RegisterDriverOption:     grpcDriverOption,
			}
			grpcCommOptions := commonoptions.NewAgentOptions()
			grpcCommOptions.HubKubeconfigDir = hubGRPCConfigDir
			grpcCommOptions.SpokeClusterName = grpcManagedClusterName
			stopGRPCAgent = runAgent(fmt.Sprintf("%s-grpc-agent", grpcTest), grpcAgentOptions, grpcCommOptions, spokeCfg)

			hubKubeConfigSecret = fmt.Sprintf("%s-hub-kubeconfig-secret-%s", grpcTest, postfix)
			hubKubeConfigDir = path.Join(util.TestDir, fmt.Sprintf("%s-kube-%s", grpcTest, postfix), "hub-kubeconfig")
			agentOptions := &spoke.SpokeAgentOptions{
				BootstrapKubeconfig:      bootstrapKubeConfigFile,
				HubKubeconfigSecret:      hubKubeConfigSecret,
				ClusterHealthCheckPeriod: 1 * time.Minute,
				RegisterDriverOption:     factory.NewOptions(),
			}
			commOptions := commonoptions.NewAgentOptions()
			commOptions.HubKubeconfigDir = hubKubeConfigDir
			commOptions.SpokeClusterName = kubeManagedClusterName

			stopKubeAgent = runAgent(fmt.Sprintf("%s-kube-agent", grpcTest), agentOptions, commOptions, spokeCfg)
		})

		ginkgo.AfterEach(func() {
			stopGRPCAgent()
			stopKubeAgent()
			stopHub()
		})

		ginkgo.It("should register managedclusters with grpc/csr successfully", func() {
			ginkgo.By("getting managedclusters and csrs after bootstrap", func() {
				assertManagedCluster(grpcManagedClusterName)
				assertManagedCluster(kubeManagedClusterName)
			})

			// simulate hub cluster admin to accept the managedcluster and approve the csr
			ginkgo.By("accept managedclusters and approve csrs", func() {
				err = util.AcceptManagedCluster(clusterClient, grpcManagedClusterName)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				err = util.AcceptManagedCluster(clusterClient, kubeManagedClusterName)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				// for gpc, the hub controller will sign the client certs, we just approve
				// the csr here
				csr, err := util.FindUnapprovedSpokeCSR(kubeClient, grpcManagedClusterName)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				err = util.ApproveCSR(kubeClient, csr)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				// for kube, we need to simulate the kube-controller-manager to sign the client
				// certs in integration test
				err = authn.ApproveSpokeClusterCSR(kubeClient, kubeManagedClusterName, time.Hour*24)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			})

			ginkgo.By("getting managedclusters joined condition", func() {
				assertManagedClusterJoined(grpcManagedClusterName, hubGRPCConfigSecret)
				assertManagedClusterJoined(kubeManagedClusterName, hubKubeConfigSecret)
			})

			ginkgo.By("getting managedclusters available condition constantly", func() {
				// after two short grace period, make sure the managed cluster is available
				gracePeriod := 2 * 5 * util.TestLeaseDurationSeconds
				assertAvailableCondition(grpcManagedClusterName, metav1.ConditionTrue, gracePeriod)
				assertAvailableCondition(kubeManagedClusterName, metav1.ConditionTrue, gracePeriod)
			})
		})
	})

	ginkgo.Context("Certificate rotation", func() {
		ginkgo.BeforeEach(func() {
			postfix = rand.String(5)
			hubOptionWithGRPC = hub.NewHubManagerOptions()
			hubOptionWithGRPC.EnabledRegistrationDrivers = []string{operatorv1.GRPCAuthType}
			hubOptionWithGRPC.GRPCCAFile = gRPCServerOptions.ClientCAFile
			hubOptionWithGRPC.GRPCCAKeyFile = gRPCCAKeyFile
			hubOptionWithGRPC.GRPCSigningDuration = 5 * time.Second
			startHub(hubOptionWithGRPC)

			grpcManagedClusterName = fmt.Sprintf("%s-cert-rotation-%s", grpcTest, postfix)

			hubGRPCConfigSecret = fmt.Sprintf("%s-hub-grpcconfig-secret-%s", grpcTest, postfix)
			hubGRPCConfigDir = path.Join(util.TestDir, fmt.Sprintf("%s-grpc-%s", grpcTest, postfix), "hub-kubeconfig")
			grpcDriverOption := factory.NewOptions()
			grpcDriverOption.RegistrationAuth = operatorv1.GRPCAuthType
			grpcDriverOption.GRPCOption = &regsgrpc.Option{
				BootstrapConfigFile: bootstrapGRPCConfigFile,
				ConfigFile:          path.Join(hubGRPCConfigDir, "config.yaml"),
			}
			grpcAgentOptions := &spoke.SpokeAgentOptions{
				BootstrapKubeconfig:      bootstrapKubeConfigFile,
				HubKubeconfigSecret:      hubGRPCConfigSecret,
				ClusterHealthCheckPeriod: 1 * time.Minute,
				RegisterDriverOption:     grpcDriverOption,
			}
			grpcCommOptions := commonoptions.NewAgentOptions()
			grpcCommOptions.HubKubeconfigDir = hubGRPCConfigDir
			grpcCommOptions.SpokeClusterName = grpcManagedClusterName
			stopGRPCAgent = runAgent(fmt.Sprintf("%s-cert-rotation-agent", grpcTest), grpcAgentOptions, grpcCommOptions, spokeCfg)
		})

		ginkgo.AfterEach(func() {
			stopGRPCAgent()
			stopHub()
		})

		ginkgo.It("should automatically rotate the certificate when it is about to expire", func() {
			ginkgo.By("getting managedclusters and csrs after bootstrap", func() {
				assertManagedCluster(grpcManagedClusterName)
			})

			// simulate hub cluster admin to accept the managedcluster and approve the csr
			ginkgo.By("accept managedclusters and approve csrs", func() {
				err = util.AcceptManagedCluster(clusterClient, grpcManagedClusterName)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				// for gpc, the hub controller will sign the client certs, we just approve
				// the csr here
				csr, err := util.FindUnapprovedSpokeCSR(kubeClient, grpcManagedClusterName)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				err = util.ApproveCSR(kubeClient, csr)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			})

			ginkgo.By("checking hub kubeconfig secret", func() {
				// the hub kubeconfig secret should be filled after the csr is approved
				gomega.Eventually(func() error {
					if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubGRPCConfigSecret); err != nil {
						return err
					}
					return nil
				}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
			})

			// the agent should rotate the certificate because the certificate with a short valid time
			// the hub controller should auto approve it
			gomega.Eventually(func() error {
				if _, err := util.FindAutoApprovedSpokeCSR(kubeClient, grpcManagedClusterName); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	})

	ginkgo.Context("Auto approval", func() {
		ginkgo.BeforeEach(func() {
			postfix = rand.String(5)
			hubOptionWithGRPC = hub.NewHubManagerOptions()
			hubOptionWithGRPC.EnabledRegistrationDrivers = []string{operatorv1.GRPCAuthType}
			hubOptionWithGRPC.GRPCCAFile = gRPCServerOptions.ClientCAFile
			hubOptionWithGRPC.GRPCCAKeyFile = gRPCCAKeyFile
			hubOptionWithGRPC.AutoApprovedGRPCUsers = []string{"test-client"}
			startHub(hubOptionWithGRPC)

			grpcManagedClusterName = fmt.Sprintf("%s-auto-approval-%s", grpcTest, postfix)

			hubGRPCConfigSecret = fmt.Sprintf("%s-hub-grpcconfig-secret-%s", grpcTest, postfix)
			hubGRPCConfigDir = path.Join(util.TestDir, fmt.Sprintf("%s-grpc-%s", grpcTest, postfix), "hub-kubeconfig")
			grpcDriverOption := factory.NewOptions()
			grpcDriverOption.RegistrationAuth = operatorv1.GRPCAuthType
			grpcDriverOption.GRPCOption = &regsgrpc.Option{
				BootstrapConfigFile: bootstrapGRPCConfigFile,
				ConfigFile:          path.Join(hubGRPCConfigDir, "config.yaml"),
			}
			grpcAgentOptions := &spoke.SpokeAgentOptions{
				BootstrapKubeconfig:      bootstrapKubeConfigFile,
				HubKubeconfigSecret:      hubGRPCConfigSecret,
				ClusterHealthCheckPeriod: 1 * time.Minute,
				RegisterDriverOption:     grpcDriverOption,
			}
			grpcCommOptions := commonoptions.NewAgentOptions()
			grpcCommOptions.HubKubeconfigDir = hubGRPCConfigDir
			grpcCommOptions.SpokeClusterName = grpcManagedClusterName
			stopGRPCAgent = runAgent(fmt.Sprintf("%s-auto-approval-agent", grpcTest), grpcAgentOptions, grpcCommOptions, spokeCfg)
		})

		ginkgo.AfterEach(func() {
			stopGRPCAgent()
			stopHub()
		})

		ginkgo.It("should automatically approve managedcluster", func() {
			ginkgo.By("getting managedclusters joined condition", func() {
				assertManagedClusterJoined(grpcManagedClusterName, hubGRPCConfigSecret)
			})
		})
	})
})

func assertManagedCluster(clusterName string) {
	gomega.Eventually(func() error {
		if _, err := util.GetManagedCluster(clusterClient, clusterName); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	gomega.Eventually(func() error {
		if _, err := util.FindUnapprovedSpokeCSR(kubeClient, clusterName); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	// the spoke cluster should has finalizer that is added by hub controller
	gomega.Eventually(func() error {
		spokeCluster, err := util.GetManagedCluster(clusterClient, clusterName)
		if err != nil {
			return err
		}

		if !commonhelpers.HasFinalizer(spokeCluster.Finalizers, clusterv1.ManagedClusterFinalizer) {
			return fmt.Errorf("finalizer is not correct")
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func assertManagedClusterJoined(clusterName, hubConfigSecret string) {
	// the managed cluster should have accepted condition after it is accepted
	gomega.Eventually(func() error {
		spokeCluster, err := util.GetManagedCluster(clusterClient, clusterName)
		if err != nil {
			return err
		}
		if !meta.IsStatusConditionTrue(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
			return fmt.Errorf("cluster should be accepted")
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	// the hub kubeconfig secret should be filled after the csr is approved
	gomega.Eventually(func() error {
		_, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubConfigSecret)
		return err
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	// the spoke cluster should have joined condition finally
	gomega.Eventually(func() error {
		spokeCluster, err := util.GetManagedCluster(clusterClient, clusterName)
		if err != nil {
			return err
		}
		if !meta.IsStatusConditionTrue(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined) {
			return fmt.Errorf("cluster should be joined")
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}
