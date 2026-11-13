package registration_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
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
