package framework

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

// CreateAndApproveKlusterlet requires operations on both hub side and spoke side
func CreateAndApproveKlusterlet(
	hub *Hub, spoke *Spoke,
	klusterletName, managedClusterName, klusterletNamespace string,
	mode operatorapiv1.InstallMode,
	bootstrapHubKubeConfigSecret *corev1.Secret,
	images Images,
	registrationDriver string,
) {
	// on the spoke side
	_, err := spoke.CreateKlusterlet(
		klusterletName,
		managedClusterName,
		klusterletNamespace,
		mode,
		bootstrapHubKubeConfigSecret,
		images,
		registrationDriver,
	)
	Expect(err).ToNot(HaveOccurred())

	// on the hub side
	Eventually(func() error {
		_, err := hub.GetManagedCluster(managedClusterName)
		return err
	}).Should(Succeed())

	Eventually(func() error {
		return hub.ApproveManagedClusterCSR(managedClusterName)
	}).Should(Succeed())

	Eventually(func() error {
		return hub.AcceptManageCluster(managedClusterName)
	}).Should(Succeed())

	Eventually(func() error {
		return hub.CheckManagedClusterStatus(managedClusterName)
	}).Should(Succeed())
}

func (spoke *Spoke) CreateKlusterlet(
	name, clusterName, klusterletNamespace string,
	mode operatorapiv1.InstallMode,
	bootstrapHubKubeConfigSecret *corev1.Secret,
	images Images,
	registrationDriver string) (*operatorapiv1.Klusterlet, error) {
	if name == "" {
		return nil, fmt.Errorf("the name should not be null")
	}
	if klusterletNamespace == "" {
		klusterletNamespace = helpers.KlusterletDefaultNamespace
	}

	var klusterlet = &operatorapiv1.Klusterlet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: operatorapiv1.KlusterletSpec{
			RegistrationImagePullSpec: images.RegistrationImage,
			WorkImagePullSpec:         images.WorkImage,
			ImagePullSpec:             images.SingletonImage,
			ExternalServerURLs: []operatorapiv1.ServerURL{
				{
					URL: "https://localhost",
				},
			},
			ClusterName: clusterName,
			Namespace:   klusterletNamespace,
			DeployOption: operatorapiv1.KlusterletDeployOption{
				Mode: mode,
			},
			WorkConfiguration: &operatorapiv1.WorkAgentConfiguration{
				StatusSyncInterval: &metav1.Duration{Duration: 5 * time.Second},
			},
		},
	}

	// Add registration configuration for gRPC driver
	if registrationDriver == "grpc" {
		klusterlet.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationConfiguration{
			RegistrationDriver: operatorapiv1.RegistrationDriver{
				AuthType: "grpc",
			},
		}
	}

	// Enable NetworkPolicies feature gate
	if klusterlet.Spec.RegistrationConfiguration == nil {
		klusterlet.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationConfiguration{}
	}
	klusterlet.Spec.RegistrationConfiguration.FeatureGates = append(
		klusterlet.Spec.RegistrationConfiguration.FeatureGates,
		operatorapiv1.FeatureGate{
			Feature: "NetworkPolicies",
			Mode:    operatorapiv1.FeatureGateModeTypeEnable,
		},
	)

	agentNamespace := helpers.AgentNamespace(klusterlet)
	klog.Infof("klusterlet: %s/%s, \t mode: %v, \t agent namespace: %s, \t registration driver: %s",
		klusterlet.Name, klusterlet.Namespace, mode, agentNamespace, registrationDriver)

	// create agentNamespace
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: agentNamespace,
			Annotations: map[string]string{
				"workload.openshift.io/allowed": "management",
			},
		},
	}
	if _, err := spoke.KubeClient.CoreV1().Namespaces().Get(context.TODO(), agentNamespace, metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("failed to get ns %v. %v", agentNamespace, err)
			return nil, err
		}

		if _, err := spoke.KubeClient.CoreV1().Namespaces().Create(context.TODO(),
			namespace, metav1.CreateOptions{}); err != nil {
			klog.Errorf("failed to create ns %v. %v", namespace, err)
			return nil, err
		}
	}

	// create bootstrap-hub-kubeconfig secret
	secret := bootstrapHubKubeConfigSecret.DeepCopy()
	if _, err := spoke.KubeClient.CoreV1().Secrets(agentNamespace).Get(context.TODO(), secret.Name, metav1.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("failed to get secret %v in ns %v. %v", secret.Name, agentNamespace, err)
			return nil, err
		}
		if _, err = spoke.KubeClient.CoreV1().Secrets(agentNamespace).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
			klog.Errorf("failed to create secret %v in ns %v. %v", secret, agentNamespace, err)
			return nil, err
		}
	}

	if helpers.IsHosted(mode) {
		// create external-managed-kubeconfig, will use the same cluster to simulate the Hosted mode.
		secret.Namespace = agentNamespace
		secret.Name = helpers.ExternalManagedKubeConfig
		if _, err := spoke.KubeClient.CoreV1().Secrets(agentNamespace).Get(context.TODO(), secret.Name, metav1.GetOptions{}); err != nil {
			if !apierrors.IsNotFound(err) {
				klog.Errorf("failed to get secret %v in ns %v. %v", secret.Name, agentNamespace, err)
				return nil, err
			}
			if _, err = spoke.KubeClient.CoreV1().Secrets(agentNamespace).Create(context.TODO(), secret, metav1.CreateOptions{}); err != nil {
				klog.Errorf("failed to create secret %v in ns %v. %v", secret, agentNamespace, err)
				return nil, err
			}
		}
	}

	// create klusterlet CR
	realKlusterlet, err := spoke.OperatorClient.OperatorV1().Klusterlets().Create(context.TODO(),
		klusterlet, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		klog.Errorf("failed to create klusterlet %v . %v", klusterlet.Name, err)
		return nil, err
	}

	return realKlusterlet, nil
}

func (spoke *Spoke) CreatePureHostedKlusterlet(name, clusterName string) (*operatorapiv1.Klusterlet, error) {
	if name == "" {
		return nil, fmt.Errorf("the name should not be null")
	}

	var klusterlet = &operatorapiv1.Klusterlet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: operatorapiv1.KlusterletSpec{
			RegistrationImagePullSpec: "quay.io/open-cluster-management/registration:latest",
			WorkImagePullSpec:         "quay.io/open-cluster-management/work:latest",
			ExternalServerURLs: []operatorapiv1.ServerURL{
				{
					URL: "https://localhost",
				},
			},
			ClusterName: clusterName,
			DeployOption: operatorapiv1.KlusterletDeployOption{
				Mode: operatorapiv1.InstallModeHosted,
			},
		},
	}

	// create klusterlet CR
	realKlusterlet, err := spoke.OperatorClient.OperatorV1().Klusterlets().Create(context.TODO(),
		klusterlet, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to create klusterlet %v . %v", klusterlet.Name, err)
		return nil, err
	}

	return realKlusterlet, nil
}

func (spoke *Spoke) CheckKlusterletStatus(klusterletName, condType, reason string, status metav1.ConditionStatus) error {
	klusterlet, err := spoke.OperatorClient.OperatorV1().Klusterlets().Get(context.TODO(), klusterletName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	cond := meta.FindStatusCondition(klusterlet.Status.Conditions, condType)
	if cond == nil {
		return fmt.Errorf("cannot find condition type %s", condType)
	}

	if cond.Reason != reason {
		return fmt.Errorf("condition reason is not matched, expect %s, got %s", reason, cond.Reason)
	}

	if cond.Status != status {
		return fmt.Errorf("condition status is not matched, expect %s, got %s", status, cond.Status)
	}

	return nil
}

func EnableRegistrationFeature(hub *Hub, spoke *Spoke, klusterletName, feature string) {
	UpdateKlusterlet(hub, spoke, klusterletName, func(klusterlet *operatorapiv1.Klusterlet) {
		setRegistrationFeatureGate(klusterlet, feature, operatorapiv1.FeatureGateModeTypeEnable)
	})
}

func RemoveRegistrationFeature(hub *Hub, spoke *Spoke, klusterletName, feature string) {
	UpdateKlusterlet(hub, spoke, klusterletName, func(klusterlet *operatorapiv1.Klusterlet) {
		setRegistrationFeatureGate(klusterlet, feature, operatorapiv1.FeatureGateModeTypeDisable)
	})
}

func setRegistrationFeatureGate(
	klusterlet *operatorapiv1.Klusterlet, feature string, mode operatorapiv1.FeatureGateModeType) {
	if klusterlet.Spec.RegistrationConfiguration == nil {
		klusterlet.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationConfiguration{}
	}

	for idx, fg := range klusterlet.Spec.RegistrationConfiguration.FeatureGates {
		if fg.Feature == feature {
			klusterlet.Spec.RegistrationConfiguration.FeatureGates[idx].Mode = mode
			return
		}
	}

	klusterlet.Spec.RegistrationConfiguration.FeatureGates = append(
		klusterlet.Spec.RegistrationConfiguration.FeatureGates,
		operatorapiv1.FeatureGate{Feature: feature, Mode: mode})
}

// UpdateKlusterlet applies update to the klusterlet and, when that changes its spec, waits until
// the operator has rolled the registration agent out with the new configuration. The tests share
// the universal klusterlet, and returning before the new agent serves leaves the next test running
// against an agent that is still restarting.
func UpdateKlusterlet(hub *Hub, spoke *Spoke, klusterletName string,
	update func(klusterlet *operatorapiv1.Klusterlet)) {
	var agentClient kubernetes.Interface
	var agentNamespace, deploymentName string
	var generation int64
	var changed bool

	Eventually(func() error {
		klusterlet, err := spoke.OperatorClient.OperatorV1().Klusterlets().Get(
			context.TODO(), klusterletName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		agentClient = AgentClient(hub, spoke, klusterlet)
		agentNamespace, deploymentName = registrationAgentDeployment(klusterlet)
		deployment, err := agentClient.AppsV1().Deployments(agentNamespace).Get(
			context.TODO(), deploymentName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		generation = deployment.Generation

		updated := klusterlet.DeepCopy()
		update(updated)
		if equality.Semantic.DeepEqual(klusterlet.Spec, updated.Spec) {
			changed = false
			return nil
		}

		_, err = spoke.OperatorClient.OperatorV1().Klusterlets().Update(
			context.TODO(), updated, metav1.UpdateOptions{})
		changed = err == nil
		return err
	}).Should(Succeed())

	if !changed {
		return
	}

	klog.Infof("waiting for the registration agent deployment %s/%s to roll out past generation %d",
		agentNamespace, deploymentName, generation)
	Eventually(func() error {
		return registrationAgentRolledOut(agentClient, agentNamespace, deploymentName, generation)
	}, 2*time.Minute, 5*time.Second).Should(Succeed())
}

// AgentClient returns the client of the cluster the klusterlet agents run on, which is the
// management cluster in the hosted modes and the managed cluster otherwise.
func AgentClient(hub *Hub, spoke *Spoke, klusterlet *operatorapiv1.Klusterlet) kubernetes.Interface {
	if helpers.IsHosted(klusterlet.Spec.DeployOption.Mode) {
		return hub.KubeClient
	}

	return spoke.KubeClient
}

// registrationAgentDeployment returns the deployment running the registration agent of the
// klusterlet. The singleton modes run all the agents in a single deployment.
func registrationAgentDeployment(klusterlet *operatorapiv1.Klusterlet) (namespace, name string) {
	if helpers.IsSingleton(klusterlet.Spec.DeployOption.Mode) {
		return helpers.AgentNamespace(klusterlet), fmt.Sprintf("%s-agent", klusterlet.Name)
	}

	return helpers.AgentNamespace(klusterlet), fmt.Sprintf("%s-registration-agent", klusterlet.Name)
}

// registrationAgentRolledOut checks that the operator has applied a generation of the registration
// agent deployment newer than previousGeneration and that all of its replicas are ready again.
func registrationAgentRolledOut(
	client kubernetes.Interface, namespace, name string, previousGeneration int64) error {
	deployment, err := client.AppsV1().Deployments(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if deployment.Generation <= previousGeneration {
		return fmt.Errorf("deployment %s/%s is still at generation %d, waiting for one newer than %d",
			namespace, name, deployment.Generation, previousGeneration)
	}

	if deployment.Status.ObservedGeneration != deployment.Generation {
		return fmt.Errorf("deployment %s/%s has observed generation %d, waiting for %d",
			namespace, name, deployment.Status.ObservedGeneration, deployment.Generation)
	}

	if deployment.Status.UpdatedReplicas != deployment.Status.Replicas {
		return fmt.Errorf("deployment %s/%s has updated %d of %d replicas",
			namespace, name, deployment.Status.UpdatedReplicas, deployment.Status.Replicas)
	}

	if deployment.Status.ReadyReplicas != deployment.Status.Replicas {
		return fmt.Errorf("deployment %s/%s has %d of %d replicas ready",
			namespace, name, deployment.Status.ReadyReplicas, deployment.Status.Replicas)
	}

	if deployment.Status.UnavailableReplicas > 0 {
		return fmt.Errorf("deployment %s/%s has %d unavailable replicas",
			namespace, name, deployment.Status.UnavailableReplicas)
	}

	return nil
}

// CleanKlusterletRelatedResources needs both hub side and spoke side operations.
func CleanKlusterletRelatedResources(
	hub *Hub, spoke *Spoke,
	klusterletName, managedClusterName string) {
	Expect(klusterletName).NotTo(Equal(""))

	// Remove addons and manifest works first; leftover ManifestWorks (e.g. pre-delete hooks) block ManagedCluster deletion.
	hub.DeleteAllManagedClusterAddOnsInCluster(managedClusterName)
	hub.WaitUntilNoManifestWorks(managedClusterName)

	// clean the managed clusters at first.
	err := hub.ClusterClient.ClusterV1().ManagedClusters().Delete(context.TODO(), managedClusterName, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		klog.Infof("managed cluster %s already absent", managedClusterName)
	} else {
		Expect(err).To(BeNil())
	}

	Eventually(func() error {
		_, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), managedClusterName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			klog.Infof("managed cluster %s deleted successfully", managedClusterName)
			return nil
		}
		if err != nil {
			klog.Infof("get managed cluster %s error: %v", klusterletName, err)
			return err
		}
		return fmt.Errorf("managed cluster %s still exists", managedClusterName)
	}).Should(Succeed())

	// clean the klusterlet
	err = spoke.OperatorClient.OperatorV1().Klusterlets().Delete(context.TODO(), klusterletName, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return
	}
	Expect(err).To(BeNil())

	Eventually(func() error {
		_, err := spoke.OperatorClient.OperatorV1().Klusterlets().Get(context.TODO(), klusterletName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			klog.Infof("klusterlet %s deleted successfully", klusterletName)
			return nil
		}
		if err != nil {
			klog.Infof("get klusterlet %s error: %v", klusterletName, err)
			return err
		}
		return fmt.Errorf("klusterlet %s still exists", klusterletName)
	}).Should(Succeed())
}
