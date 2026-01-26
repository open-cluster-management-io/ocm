package framework

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func (spoke *Spoke) EnableRegistrationFeature(klusterletName, feature string) error {
	kl, err := spoke.OperatorClient.OperatorV1().Klusterlets().Get(context.TODO(), klusterletName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if kl.Spec.RegistrationConfiguration == nil {
		kl.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationConfiguration{}
	}

	if len(kl.Spec.RegistrationConfiguration.FeatureGates) == 0 {
		kl.Spec.RegistrationConfiguration.FeatureGates = make([]operatorapiv1.FeatureGate, 0)
	}

	for idx, f := range kl.Spec.RegistrationConfiguration.FeatureGates {
		if f.Feature == feature {
			if f.Mode == operatorapiv1.FeatureGateModeTypeEnable {
				return nil
			}
			kl.Spec.RegistrationConfiguration.FeatureGates[idx].Mode = operatorapiv1.FeatureGateModeTypeEnable
			_, err = spoke.OperatorClient.OperatorV1().Klusterlets().Update(context.TODO(), kl, metav1.UpdateOptions{})
			return err
		}
	}

	featureGate := operatorapiv1.FeatureGate{
		Feature: feature,
		Mode:    operatorapiv1.FeatureGateModeTypeEnable,
	}

	kl.Spec.RegistrationConfiguration.FeatureGates = append(kl.Spec.RegistrationConfiguration.FeatureGates, featureGate)
	_, err = spoke.OperatorClient.OperatorV1().Klusterlets().Update(context.TODO(), kl, metav1.UpdateOptions{})
	return err
}

func (spoke *Spoke) RemoveRegistrationFeature(klusterletName string, feature string) error {
	kl, err := spoke.OperatorClient.OperatorV1().Klusterlets().Get(context.TODO(), klusterletName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	for indx, fg := range kl.Spec.RegistrationConfiguration.FeatureGates {
		if fg.Feature == feature {
			kl.Spec.RegistrationConfiguration.FeatureGates[indx].Mode = operatorapiv1.FeatureGateModeTypeDisable
			break
		}
	}
	_, err = spoke.OperatorClient.OperatorV1().Klusterlets().Update(context.TODO(), kl, metav1.UpdateOptions{})
	return err
}

// CleanKlusterletRelatedResources needs both hub side and spoke side operations
func CleanKlusterletRelatedResources(
	hub *Hub, spoke *Spoke,
	klusterletName, managedClusterName string) {
	Expect(klusterletName).NotTo(Equal(""))

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
