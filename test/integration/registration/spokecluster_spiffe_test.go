package registration_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"
	operatorv1 "open-cluster-management.io/api/operator/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/testing/fakespire"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/hub"
	hubspiffe "open-cluster-management.io/ocm/pkg/registration/hub/spiffe"
	"open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	ocmgrpcserver "open-cluster-management.io/ocm/pkg/server/grpc"
	workspoke "open-cluster-management.io/ocm/pkg/work/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

const (
	spiffeTD    = "ocm-hub.example.org"
	spiffeHubID = "spiffe://ocm-hub.example.org/ocm/hub/grpc-server"
)

func registrationID(cluster string) string {
	return fmt.Sprintf("spiffe://%s/ocm/cluster/%s/agent/registration", spiffeTD, cluster)
}

// startSPIFFEServer runs the hub gRPC server with only the SPIFFE listener, as the
// cluster-manager operator renders it for spiffe, against the envtest kube-apiserver.
func startSPIFFEServer(ctx context.Context, socket string, agentTemplate string) (string, <-chan error) {
	port, err := util.FreeLocalPort()
	Expect(err).NotTo(HaveOccurred())
	opts := ocmgrpcserver.NewGRPCServerOptions()
	opts.GRPCEnabled = false
	opts.SPIFFE.Enabled = true
	opts.SPIFFE.ListenAddress = "127.0.0.1:" + port
	opts.SPIFFE.TrustDomain = spiffeTD
	opts.SPIFFE.WorkloadAPISocket = socket
	if agentTemplate != "" {
		opts.SPIFFE.AgentIDTemplate = agentTemplate
	}
	done := make(chan error, 1)
	go func() {
		done <- opts.Run(ctx, &controllercmd.ControllerContext{KubeConfig: hubCfg})
	}()
	addr := "127.0.0.1:" + port
	Eventually(func() error {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = conn.Close()
		}
		return err
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	return addr, done
}

func looksLikeKeyOrCert(b []byte) bool {
	return bytes.Contains(b, []byte("PRIVATE KEY")) || bytes.Contains(b, []byte("BEGIN CERTIFICATE"))
}

// autoApprovedCluster is the one cluster whose agent the SPIFFE hub auto-approves.
const autoApprovedCluster = "spiffe-auto-approved"

var _ = Describe("Registration using SPIFFE", Ordered, Label("spiffe-test"), func() {
	var spiffeCA *fakespire.CA
	var hubAddr string
	var stopServer context.CancelFunc
	var serverDone <-chan error

	// newAgent starts a fake Workload API serving id, and an agent for cluster with it.
	newAgent := func(cluster, id string, ttl time.Duration) (*util.SPIFFEAgent, *fakespire.Server) {
		api := fakespire.Start(GinkgoTB(), spiffeCA)
		api.SetSVIDs(spiffeCA.Issue(GinkgoTB(), id, ttl))
		agent, err := util.NewSPIFFEAgent(context.Background(), hubAddr, spiffeHubID, id, api.Addr, cluster)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(agent.Close)
		return agent, api
	}

	permissionDenied := func(err error) bool {
		return err != nil && status.Code(err) == codes.PermissionDenied
	}

	accept := func(cluster string) {
		Eventually(func() error {
			mc, err := clusterClient.ClusterV1().ManagedClusters().Get(context.Background(), cluster, metav1.GetOptions{})
			if err != nil {
				return err
			}
			mc.Spec.HubAcceptsClient = true
			_, err = clusterClient.ClusterV1().ManagedClusters().Update(context.Background(), mc, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	}

	renewLease := func(agent *util.SPIFFEAgent) error {
		lease, err := kubeClient.CoordinationV1().Leases(agent.Cluster).Get(context.Background(),
			"managed-cluster-lease", metav1.GetOptions{})
		if err != nil {
			return err
		}
		return agent.UpdateLease(context.Background(), lease)
	}

	BeforeAll(func() {
		stopHub()
		Expect(features.HubMutableFeatureGate.Set("SPIFFERegistration=true")).To(Succeed())
		// The work agent spec runs a work agent, whose feature gates this suite doesn't register.
		Expect(features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates)).To(Succeed())

		// The pre-acceptance permission the cluster-manager operator binds for spiffe.
		ctx := context.Background()
		_, err := kubeClient.RbacV1().ClusterRoles().Create(ctx, &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: "open-cluster-management:spiffe-agent"},
			Rules: []rbacv1.PolicyRule{{APIGroups: []string{clusterv1.GroupName}, Resources: []string{"managedclusters"},
				Verbs: []string{"get", "create", "list", "watch"}}},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = kubeClient.RbacV1().ClusterRoleBindings().Create(ctx, &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "open-cluster-management:spiffe-agent"},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "open-cluster-management:spiffe-agent"},
			Subjects: []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "Group",
				Name: "system:open-cluster-management:managed-clusters"}},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		opts := hub.NewHubManagerOptions()
		opts.EnabledRegistrationDrivers = []string{operatorv1.CSRAuthType, operatorv1.SPIFFEAuthType}
		opts.SPIFFEMaxSVIDLifetime = 10 * time.Second
		opts.SPIFFEMaxClockSkew = time.Second
		// The suite turns ManagedClusterAutoApproval on: only this cluster's agent is auto-approved.
		opts.SPIFFEAutoApprovedIdentities = []string{registrationID(autoApprovedCluster)}
		startHub(opts)

		spiffeCA = fakespire.NewCA(GinkgoTB(), spiffeTD)
		hubAPI := fakespire.Start(GinkgoTB(), spiffeCA)
		hubAPI.SetSVIDs(spiffeCA.Issue(GinkgoTB(), spiffeHubID, time.Hour))
		var serverCtx context.Context
		serverCtx, stopServer = context.WithCancel(context.Background())
		hubAddr, serverDone = startSPIFFEServer(serverCtx, hubAPI.Addr, "")
	})

	AfterAll(func() {
		stopServer()
		Eventually(serverDone, 15*time.Second).Should(Receive(BeNil()), "the SPIFFE server stops")
		Expect(features.HubMutableFeatureGate.Set("SPIFFERegistration=false")).To(Succeed())
		ctx := context.Background()
		_ = kubeClient.RbacV1().ClusterRoleBindings().Delete(ctx, "open-cluster-management:spiffe-agent", metav1.DeleteOptions{})
		_ = kubeClient.RbacV1().ClusterRoles().Delete(ctx, "open-cluster-management:spiffe-agent", metav1.DeleteOptions{})
		// Stop the SPIFFE hub before restarting the default one: startHub replaces stopHub.
		stopHub()
		startHub(hubOption)
	})

	It("auto-approves only the clusters of the auto-approved SPIFFE IDs", func() {
		ctx := context.Background()
		By("a cluster registered by an auto-approved SPIFFE ID is accepted")
		agent, _ := newAgent(autoApprovedCluster, registrationID(autoApprovedCluster), time.Hour)
		Expect(agent.CreateCluster(ctx, autoApprovedCluster, nil)).To(Succeed())
		Eventually(func() bool {
			mc, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, autoApprovedCluster, metav1.GetOptions{})
			return err == nil && mc.Spec.HubAcceptsClient &&
				mc.Annotations[clusterv1.SPIFFERegisteredByAnnotationKey] == registrationID(autoApprovedCluster)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("any other spiffe cluster waits for the admin, although the feature gate is on")
		other := fmt.Sprintf("spiffe-%s", rand.String(5))
		otherAgent, _ := newAgent(other, registrationID(other), time.Hour)
		Expect(otherAgent.CreateCluster(ctx, other, nil)).To(Succeed())
		Consistently(func() bool {
			mc, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, other, metav1.GetOptions{})
			return err == nil && !mc.Spec.HubAcceptsClient
		}, 5*time.Second, eventuallyInterval).Should(BeTrue())
	})

	It("a hub with an invalid auto-approved identity doesn't start", func() {
		opts := hub.NewHubManagerOptions()
		opts.EnabledRegistrationDrivers = []string{operatorv1.SPIFFEAuthType}
		opts.SPIFFEAutoApprovedIdentities = []string{"spiffe://ocm-hub.example.org/ocm/[cluster"}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := opts.RunControllerManager(ctx, &controllercmd.ControllerContext{
			KubeConfig: hubCfg, EventRecorder: util.NewIntegrationTestEventRecorder("invalid-hub")})
		Expect(err).To(MatchError(ContainSubstring("auto-approved SPIFFE ID")))
	})

	It("registers, is pinned, acts only for its own cluster and survives SVID rotation", func() {
		ctx := context.Background()
		cluster := fmt.Sprintf("spiffe-%s", rand.String(5))
		other := fmt.Sprintf("spiffe-%s", rand.String(5))
		agent, api := newAgent(cluster, registrationID(cluster), 3*time.Second)
		otherAgent, _ := newAgent(other, registrationID(other), time.Hour)

		By("an agent can't create another agent's cluster")
		Expect(permissionDenied(otherAgent.CreateCluster(ctx, cluster, nil))).To(BeTrue())

		By("the agent creates its ManagedCluster, pinned to its SVID's trust domain")
		Expect(agent.CreateCluster(ctx, cluster, map[string]string{clusterv1.SPIFFETrustDomainAnnotationKey: "spoofed.example.org"})).To(Succeed())
		mc, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, cluster, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(mc.Spec.HubAcceptsClient).To(BeFalse())
		Expect(mc.Annotations).To(HaveKeyWithValue(clusterv1.SPIFFETrustDomainAnnotationKey, spiffeTD))
		Eventually(func() []string {
			mc, _ := clusterClient.ClusterV1().ManagedClusters().Get(ctx, cluster, metav1.GetOptions{})
			return mc.Finalizers
		}, eventuallyTimeout, eventuallyInterval).Should(ContainElement(hubspiffe.RevocationFinalizer))

		By("the admin accepts it; the agent renews its lease")
		accept(cluster)
		Eventually(func() error { return renewLease(agent) }, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("another agent can't renew it")
		Eventually(func() error { return renewLease(agent) }, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		lease, err := kubeClient.CoordinationV1().Leases(cluster).Get(ctx, "managed-cluster-lease", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(permissionDenied(otherAgent.UpdateLease(ctx, lease))).To(BeTrue())

		By("SPIRE rotates the SVID; after the old one expires, the agent reconnects with the new one")
		api.SetSVIDs(spiffeCA.Issue(GinkgoTB(), registrationID(cluster), time.Hour))
		time.Sleep(4 * time.Second)
		Eventually(func() error { return renewLease(agent) }, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	})

	It("is cut off when the cluster is denied", func() {
		ctx := context.Background()
		cluster := fmt.Sprintf("spiffe-%s", rand.String(5))
		agent, _ := newAgent(cluster, registrationID(cluster), time.Hour)
		Expect(agent.CreateCluster(ctx, cluster, nil)).To(Succeed())
		accept(cluster)
		Eventually(func() error { return renewLease(agent) }, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		watchCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		done, err := agent.WatchCluster(watchCtx)
		Expect(err).NotTo(HaveOccurred())

		By("deny: the stream ends and the lease can't be renewed")
		Eventually(func() error {
			mc, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, cluster, metav1.GetOptions{})
			if err != nil {
				return err
			}
			mc.Spec.HubAcceptsClient = false
			_, err = clusterClient.ClusterV1().ManagedClusters().Update(ctx, mc, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		Eventually(done, eventuallyTimeout).Should(Receive())
		Eventually(func() bool { return permissionDenied(renewLease(agent)) }, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	})

	It("can't recreate a deleted cluster until the delete guard releases it", func() {
		ctx := context.Background()
		cluster := fmt.Sprintf("spiffe-%s", rand.String(5))
		agent, _ := newAgent(cluster, registrationID(cluster), time.Hour)
		Expect(agent.CreateCluster(ctx, cluster, nil)).To(Succeed())
		accept(cluster)
		Eventually(func() error { return renewLease(agent) }, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		watchCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		done, err := agent.WatchCluster(watchCtx)
		Expect(err).NotTo(HaveOccurred())

		By("delete: the cluster stays terminating and its agent is refused")
		Expect(clusterClient.ClusterV1().ManagedClusters().Delete(ctx, cluster, metav1.DeleteOptions{})).To(Succeed())
		mc, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, cluster, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(mc.DeletionTimestamp).NotTo(BeNil())
		Expect(mc.Finalizers).To(ContainElement(hubspiffe.RevocationFinalizer))
		Eventually(done, eventuallyTimeout).Should(Receive())
		err = agent.CreateCluster(ctx, cluster, nil)
		Expect(permissionDenied(err)).To(BeTrue(), "got %v", err)
		Expect(err.Error()).To(ContainSubstring("is being deleted"))

		By("after maxSVIDLifetime + maxClockSkew (11s) the cluster is gone")
		Eventually(func() bool {
			_, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, cluster, metav1.GetOptions{})
			return apierrors.IsNotFound(err)
		}, 60*time.Second, eventuallyInterval).Should(BeTrue())

		By("an agent with a new SVID may create it again")
		fresh, _ := newAgent(cluster, registrationID(cluster), time.Hour)
		Eventually(func() error { return fresh.CreateCluster(ctx, cluster, nil) }, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	})

	It("accepts only the configured agent ID template", func() {
		ctx := context.Background()
		hubAPI := fakespire.Start(GinkgoTB(), spiffeCA)
		hubAPI.SetSVIDs(spiffeCA.Issue(GinkgoTB(), spiffeHubID, time.Hour))
		serverCtx, stop := context.WithCancel(ctx)
		defer stop()
		addr, done := startSPIFFEServer(serverCtx, hubAPI.Addr, "spiffe://{trustDomain}/k8s/{cluster}/ns/{namespace}/ocm/{role}")
		defer func() {
			stop()
			Eventually(done, 15*time.Second).Should(Receive(BeNil()), "the template server stops")
		}()

		cluster := fmt.Sprintf("spiffe-%s", rand.String(5))
		custom := fmt.Sprintf("spiffe://%s/k8s/%s/ns/team-a-ocm/ocm/registration", spiffeTD, cluster)
		api := fakespire.Start(GinkgoTB(), spiffeCA)
		api.SetSVIDs(spiffeCA.Issue(GinkgoTB(), custom, time.Hour), spiffeCA.Issue(GinkgoTB(), registrationID(cluster), time.Hour))

		agent, err := util.NewSPIFFEAgent(ctx, addr, spiffeHubID, custom, api.Addr, cluster)
		Expect(err).NotTo(HaveOccurred())
		defer agent.Close()
		Expect(agent.CreateCluster(ctx, cluster, nil)).To(Succeed())

		By("the default layout is refused by this server")
		defaultAgent, err := util.NewSPIFFEAgent(ctx, addr, spiffeHubID, registrationID(cluster), api.Addr, cluster)
		Expect(err).NotTo(HaveOccurred())
		defer defaultAgent.Close()
		err = defaultAgent.CreateCluster(ctx, cluster, nil)
		Expect(err).To(HaveOccurred())
	})

	It("an agent started before its SPIRE entry waits for its SVID", func() {
		ctx := context.Background()
		cluster := fmt.Sprintf("spiffe-wait-%s", rand.String(5))
		api := fakespire.Start(GinkgoTB(), spiffeCA) // no SVID for the agent yet

		newAgentOptions := func() (*spoke.SpokeAgentOptions, *commonoptions.AgentOptions) {
			driverOption := factory.NewOptions()
			driverOption.RegistrationAuth = operatorv1.SPIFFEAuthType
			driverOption.AddonKubeClientRegistrationAuth = operatorv1.SPIFFEAuthType
			driverOption.SPIFFEOption.HubEndpoint = hubAddr
			driverOption.SPIFFEOption.HubSPIFFEID = spiffeHubID
			driverOption.SPIFFEOption.AgentSPIFFEID = registrationID(cluster)
			driverOption.SPIFFEOption.WorkloadAPISocket = api.Addr
			commOptions := commonoptions.NewAgentOptions()
			commOptions.HubKubeconfigDir = path.Join(util.TestDir, cluster, "hub-kubeconfig")
			commOptions.SpokeClusterName = cluster
			return &spoke.SpokeAgentOptions{
				HubKubeconfigSecret:      fmt.Sprintf("%s-hub-kubeconfig-secret", cluster),
				ClusterHealthCheckPeriod: time.Minute,
				RegisterDriverOption:     driverOption,
			}, commOptions
		}

		By("an agent stopped while it waits exits without creating anything")
		agentOptions, commOptions := newAgentOptions()
		stopAgent := runAgent(cluster, agentOptions, commOptions, spokeCfg)
		time.Sleep(2 * time.Second)
		stopAgent()
		Consistently(func() bool {
			_, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, cluster, metav1.GetOptions{})
			return apierrors.IsNotFound(err)
		}, 3*time.Second, eventuallyInterval).Should(BeTrue())

		By("an agent that gets its SVID later joins")
		agentOptions, commOptions = newAgentOptions()
		stopAgent = runAgent(cluster, agentOptions, commOptions, spokeCfg)
		defer stopAgent()
		time.Sleep(2 * time.Second)
		api.SetSVIDs(spiffeCA.Issue(GinkgoTB(), registrationID(cluster), time.Hour))
		Eventually(func() error {
			_, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, cluster, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
	})

	It("the registration agent joins with its SVID only", func() {
		ctx := context.Background()
		cluster := fmt.Sprintf("spiffe-agent-%s", rand.String(5))
		api := fakespire.Start(GinkgoTB(), spiffeCA)
		// The platform may give the pod other SVIDs too; the agent picks its own by ID.
		api.SetSVIDs(
			spiffeCA.Issue(GinkgoTB(), "spiffe://ocm-hub.example.org/ns/open-cluster-management-agent/sa/default", time.Hour),
			spiffeCA.Issue(GinkgoTB(), registrationID(cluster), time.Hour))

		driverOption := factory.NewOptions()
		driverOption.RegistrationAuth = operatorv1.SPIFFEAuthType
		driverOption.AddonKubeClientRegistrationAuth = operatorv1.SPIFFEAuthType
		driverOption.SPIFFEOption.HubEndpoint = hubAddr
		driverOption.SPIFFEOption.HubSPIFFEID = spiffeHubID
		driverOption.SPIFFEOption.AgentSPIFFEID = registrationID(cluster)
		driverOption.SPIFFEOption.WorkloadAPISocket = api.Addr
		secretName := fmt.Sprintf("%s-hub-kubeconfig-secret", cluster)
		agentOptions := &spoke.SpokeAgentOptions{
			HubKubeconfigSecret:      secretName,
			ClusterHealthCheckPeriod: time.Minute,
			RegisterDriverOption:     driverOption,
		}
		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = path.Join(util.TestDir, cluster, "hub-kubeconfig")
		commOptions.SpokeClusterName = cluster
		stopAgent := runAgent(cluster, agentOptions, commOptions, spokeCfg)
		defer stopAgent()

		By("the agent creates its cluster, pinned to its trust domain")
		Eventually(func() error {
			mc, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, cluster, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if mc.Annotations[clusterv1.SPIFFETrustDomainAnnotationKey] != spiffeTD {
				return fmt.Errorf("annotations %v", mc.Annotations)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("once accepted, the agent joins and keeps its lease fresh")
		accept(cluster)
		Eventually(func() error {
			mc, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, cluster, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if !meta.IsStatusConditionTrue(mc.Status.Conditions, clusterv1.ManagedClusterConditionJoined) {
				return fmt.Errorf("not joined: %v", mc.Status.Conditions)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		Eventually(func() error {
			lease, err := kubeClient.CoordinationV1().Leases(cluster).Get(ctx, "managed-cluster-lease", metav1.GetOptions{})
			if err != nil {
				return err
			}
			if lease.Spec.RenewTime == nil || time.Since(lease.Spec.RenewTime.Time) > 30*time.Second {
				return fmt.Errorf("lease not renewed: %v", lease.Spec.RenewTime)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("no hub kubeconfig Secret exists on the managed cluster")
		_, err := kubeClient.CoreV1().Secrets(commOptions.ComponentNamespace).Get(ctx, secretName, metav1.GetOptions{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "got %v", err)
	})

	It("the work agent applies ManifestWorks with its own SVID", func() {
		ctx := context.Background()
		cluster := fmt.Sprintf("spiffe-work-%s", rand.String(5))
		registration, _ := newAgent(cluster, registrationID(cluster), time.Hour)
		Expect(registration.CreateCluster(ctx, cluster, nil)).To(Succeed())
		accept(cluster)
		Eventually(func() error {
			_, err := kubeClient.CoreV1().Namespaces().Get(ctx, cluster, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		workID := fmt.Sprintf("spiffe://%s/ocm/cluster/%s/agent/work", spiffeTD, cluster)
		api := fakespire.Start(GinkgoTB(), spiffeCA)
		api.SetSVIDs(spiffeCA.Issue(GinkgoTB(), workID, time.Hour))
		workOptions := workspoke.NewWorkloadAgentOptions()
		workOptions.SPIFFE.HubEndpoint = hubAddr
		workOptions.SPIFFE.HubSPIFFEID = spiffeHubID
		workOptions.SPIFFE.AgentSPIFFEID = workID
		workOptions.SPIFFE.WorkloadAPISocket = api.Addr
		workOptions.StatusSyncInterval = 3 * time.Second
		commOptions := commonoptions.NewAgentOptions()
		commOptions.SpokeClusterName = cluster
		commOptions.AgentID = cluster + "-work"
		workCtx, stopWork := context.WithCancel(ctx)
		defer stopWork()
		go func() {
			defer GinkgoRecover()
			_ = workspoke.NewWorkAgentConfig(commOptions, workOptions).RunWorkloadAgent(workCtx,
				&controllercmd.ControllerContext{KubeConfig: spokeCfg, EventRecorder: util.NewIntegrationTestEventRecorder("work")})
		}()

		By("a ManifestWork on the hub is applied on the managed cluster")
		cmName := "cm-" + cluster
		cm := &corev1.ConfigMap{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
			ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: "default"},
			Data:       map[string]string{"a": "b"},
		}
		work := &workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{Name: "work1", Namespace: cluster},
			Spec: workv1.ManifestWorkSpec{Workload: workv1.ManifestsTemplate{
				Manifests: []workv1.Manifest{{RawExtension: runtime.RawExtension{Object: cm}}},
			}},
		}
		_, err := workClient.WorkV1().ManifestWorks(cluster).Create(ctx, work, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() error {
			_, err := kubeClient.CoreV1().ConfigMaps("default").Get(ctx, cmName, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("its status reaches the hub")
		Eventually(func() bool {
			w, err := workClient.WorkV1().ManifestWorks(cluster).Get(ctx, "work1", metav1.GetOptions{})
			return err == nil && meta.IsStatusConditionTrue(w.Status.Conditions, workv1.WorkApplied)
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	})

	It("an add-on agent acts for its own add-on only, until the add-on is disabled", func() {
		ctx := context.Background()
		cluster := fmt.Sprintf("spiffe-addon-%s", rand.String(5))
		registration, _ := newAgent(cluster, registrationID(cluster), time.Hour)
		Expect(registration.CreateCluster(ctx, cluster, nil)).To(Succeed())
		accept(cluster)
		Eventually(func() error {
			_, err := kubeClient.CoreV1().Namespaces().Get(ctx, cluster, metav1.GetOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		addonID := func(addon string) string {
			return fmt.Sprintf("spiffe://%s/ocm/cluster/%s/addon/%s/agent/%s-agent", spiffeTD, cluster, addon, addon)
		}
		for _, name := range []string{"my-addon", "other-addon"} {
			_, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(cluster).Create(ctx, &addonv1beta1.ManagedClusterAddOn{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cluster}}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			// The hub RBAC the add-on manager creates for the add-on's groups.
			group := fmt.Sprintf("system:open-cluster-management:cluster:%s:addon:%s", cluster, name)
			_, err = kubeClient.RbacV1().Roles(cluster).Create(ctx, &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{Name: name},
				// The SubjectAccessReview for add-on events carries no resource name, so no
				// resourceNames here; the SPIFFE check refuses another add-on by name.
				Rules: []rbacv1.PolicyRule{{APIGroups: []string{addonv1beta1.GroupName}, Resources: []string{"managedclusteraddons", "managedclusteraddons/status"},
					Verbs: []string{"get", "list", "watch", "update", "patch"}}},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			_, err = kubeClient.RbacV1().RoleBindings(cluster).Create(ctx, &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: name},
				RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: name},
				Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "Group", Name: group}},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
		}
		agent, _ := newAgent(cluster, addonID("my-addon"), time.Hour)
		update := func(name string) error {
			mca, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(cluster).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			mca.Status.Namespace = "my-addon-ns"
			return agent.UpdateAddOnStatus(ctx, mca)
		}

		By("the add-on agent updates its own add-on's status")
		Eventually(func() error { return update("my-addon") }, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("but not another add-on's")
		Expect(permissionDenied(update("other-addon"))).To(BeTrue())

		By("disabling the add-on cuts its agent off")
		Expect(addOnClient.AddonV1beta1().ManagedClusterAddOns(cluster).Delete(ctx, "my-addon", metav1.DeleteOptions{})).To(Succeed())
		Eventually(func() bool {
			mca := &addonv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Name: "my-addon", Namespace: cluster}}
			return permissionDenied(agent.UpdateAddOnStatus(ctx, mca))
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	})

	It("a cluster moves from csr to spiffe and back without losing anything", func() {
		ctx := context.Background()
		cluster := fmt.Sprintf("spiffe-migrate-%s", rand.String(5))
		hubSecret := cluster + "-hub-kubeconfig-secret"
		bootstrapSecret := cluster + "-bootstrap-hub-kubeconfig"
		hubKubeconfigDir := path.Join(util.TestDir, cluster, "hub-kubeconfig")
		addOnNamespace := cluster + "-addon"
		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = cluster

		joinWithCSR := func() context.CancelFunc {
			stop := runAgent(cluster, &spoke.SpokeAgentOptions{
				BootstrapKubeconfig:      bootstrapKubeConfigFile,
				HubKubeconfigSecret:      hubSecret,
				ClusterHealthCheckPeriod: time.Minute,
				RegisterDriverOption:     factory.NewOptions(),
			}, commOptions, spokeCfg)
			Eventually(func() error {
				return authn.ApproveSpokeClusterCSR(kubeClient, cluster, 24*time.Hour)
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
			Eventually(func() error {
				_, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubSecret)
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
			return stop
		}
		joined := func() error {
			mc, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, cluster, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if !meta.IsStatusConditionTrue(mc.Status.Conditions, clusterv1.ManagedClusterConditionJoined) {
				return fmt.Errorf("not joined: %v", mc.Status.Conditions)
			}
			return nil
		}
		clusterCSRs := func() int {
			csrs, err := kubeClient.CertificatesV1().CertificateSigningRequests().List(ctx, metav1.ListOptions{
				LabelSelector: clusterv1.ClusterNameLabelKey + "=" + cluster})
			Expect(err).NotTo(HaveOccurred())
			return len(csrs.Items)
		}
		roleBindings := func() map[string]string {
			rbs, err := kubeClient.RbacV1().RoleBindings(cluster).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			out := map[string]string{}
			for _, rb := range rbs.Items {
				out[rb.Name] = string(rb.UID)
			}
			return out
		}

		By("the cluster joins with csr")
		stopCSRAgent := joinWithCSR()
		Expect(util.AcceptManagedCluster(clusterClient, cluster)).To(Succeed())
		Eventually(joined, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("it has a ManifestWork and an add-on whose agent used a hub kubeconfig Secret")
		work, err := workClient.WorkV1().ManifestWorks(cluster).Create(ctx, &workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{Name: "w1", Namespace: cluster}}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		mca, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(cluster).Create(ctx, &addonv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{Name: "my-addon", Namespace: cluster}}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		// What the add-on manager reports once the add-on is installed in a dedicated namespace.
		mca.Status.Namespace = addOnNamespace
		mca.Status.Registrations = []addonv1beta1.RegistrationConfig{{Type: addonv1beta1.KubeClient,
			KubeClient: &addonv1beta1.KubeClientConfig{Subject: addonv1beta1.KubeClientSubject{BaseSubject: addonv1beta1.BaseSubject{
				User: fmt.Sprintf("system:open-cluster-management:cluster:%s:addon:my-addon:agent:my-addon", cluster)}}}}}
		_, err = addOnClient.AddonV1beta1().ManagedClusterAddOns(cluster).UpdateStatus(ctx, mca, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: addOnNamespace,
			Labels: map[string]string{"open-cluster-management.io/spiffe-addon-namespace": "klusterlet"}}}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		for _, sec := range []*corev1.Secret{
			{ObjectMeta: metav1.ObjectMeta{Name: "my-addon-hub-kubeconfig", Namespace: addOnNamespace}},
			{ObjectMeta: metav1.ObjectMeta{Name: bootstrapSecret, Namespace: testNamespace}},
		} {
			_, err = kubeClient.CoreV1().Secrets(sec.Namespace).Create(ctx, sec, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
		}
		before, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, cluster, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		rbsBefore, csrsBefore := roleBindings(), clusterCSRs()
		Expect(rbsBefore).NotTo(BeEmpty())

		By("its agent switches to spiffe")
		stopCSRAgent()
		api := fakespire.Start(GinkgoTB(), spiffeCA)
		api.SetSVIDs(spiffeCA.Issue(GinkgoTB(), registrationID(cluster), time.Hour))
		driverOption := factory.NewOptions()
		driverOption.RegistrationAuth = operatorv1.SPIFFEAuthType
		driverOption.AddonKubeClientRegistrationAuth = operatorv1.SPIFFEAuthType
		driverOption.SPIFFEOption.HubEndpoint = hubAddr
		driverOption.SPIFFEOption.HubSPIFFEID = spiffeHubID
		driverOption.SPIFFEOption.AgentSPIFFEID = registrationID(cluster)
		driverOption.SPIFFEOption.WorkloadAPISocket = api.Addr
		driverOption.SPIFFEOption.TrustDomain = spiffeTD
		stopSPIFFEAgent := runAgent(cluster, &spoke.SpokeAgentOptions{
			HubKubeconfigSecret:       hubSecret,
			BootstrapKubeconfigSecret: bootstrapSecret,
			ClusterHealthCheckPeriod:  time.Minute,
			RegisterDriverOption:      driverOption,
		}, commOptions, spokeCfg)

		By("the old hub credentials go once the cluster is reachable with the SVID")
		for _, s := range []struct{ ns, name string }{
			{testNamespace, hubSecret}, {testNamespace, bootstrapSecret}, {addOnNamespace, "my-addon-hub-kubeconfig"},
		} {
			Eventually(func() bool {
				_, err := kubeClient.CoreV1().Secrets(s.ns).Get(ctx, s.name, metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue(), "Secret %s/%s", s.ns, s.name)
		}
		Eventually(func() error {
			lease, err := kubeClient.CoordinationV1().Leases(cluster).Get(ctx, "managed-cluster-lease", metav1.GetOptions{})
			if err != nil {
				return err
			}
			if lease.Spec.RenewTime == nil || time.Since(lease.Spec.RenewTime.Time) > 30*time.Second {
				return fmt.Errorf("lease not renewed: %v", lease.Spec.RenewTime)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		By("nothing on the hub was recreated")
		after, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, cluster, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(after.UID).To(Equal(before.UID), "ManagedCluster must not be recreated")
		Expect(meta.IsStatusConditionTrue(after.Status.Conditions, clusterv1.ManagedClusterConditionJoined)).To(BeTrue())
		workAfter, err := workClient.WorkV1().ManifestWorks(cluster).Get(ctx, "w1", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(workAfter.UID).To(Equal(work.UID))
		mcaAfter, err := addOnClient.AddonV1beta1().ManagedClusterAddOns(cluster).Get(ctx, "my-addon", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(mcaAfter.UID).To(Equal(mca.UID))
		Expect(roleBindings()).To(Equal(rbsBefore))
		Expect(clusterCSRs()).To(Equal(csrsBefore), "no CSR with spiffe")

		By("it moves back to csr with a new bootstrap kubeconfig")
		stopSPIFFEAgent()
		// A new agent pod mounts the (now absent) hub kubeconfig Secret: its directory is empty.
		Expect(os.RemoveAll(hubKubeconfigDir)).To(Succeed())
		stopCSRAgent = joinWithCSR()
		defer stopCSRAgent()
		Eventually(joined, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		back, err := clusterClient.ClusterV1().ManagedClusters().Get(ctx, cluster, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(back.UID).To(Equal(before.UID), "ManagedCluster must not be recreated")
	})

	It("leaves no Secret or ConfigMap with key material in the SPIFFE clusters' namespaces", func() {
		ctx := context.Background()
		clusters, err := clusterClient.ClusterV1().ManagedClusters().List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		checked := 0
		for _, mc := range clusters.Items {
			if _, ok := mc.Annotations[clusterv1.SPIFFETrustDomainAnnotationKey]; !ok {
				continue
			}
			checked++
			secrets, err := kubeClient.CoreV1().Secrets(mc.Name).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			for _, s := range secrets.Items {
				for k, v := range s.Data {
					Expect(looksLikeKeyOrCert(v)).To(BeFalse(), "%s/%s key %s", mc.Name, s.Name, k)
				}
			}
			configMaps, err := kubeClient.CoreV1().ConfigMaps(mc.Name).List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			for _, cm := range configMaps.Items {
				// kube-root-ca.crt is the kube-apiserver's own CA, put in every namespace by kube.
				if cm.Name == "kube-root-ca.crt" {
					continue
				}
				for k, v := range cm.Data {
					Expect(looksLikeKeyOrCert([]byte(v))).To(BeFalse(), "%s/%s key %s", mc.Name, cm.Name, k)
				}
			}
		}
		Expect(checked).To(BeNumerically(">", 0))
	})
})
