package framework

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocmfeature "open-cluster-management.io/api/feature"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

func (hub *Hub) EnableHubRegistrationFeature(feature string) error {
	cm, err := hub.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), hub.ClusterManagerName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if cm.Spec.RegistrationConfiguration == nil {
		cm.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{}
	}

	if len(cm.Spec.RegistrationConfiguration.FeatureGates) == 0 {
		cm.Spec.RegistrationConfiguration.FeatureGates = make([]operatorapiv1.FeatureGate, 0)
	}

	for idx, f := range cm.Spec.RegistrationConfiguration.FeatureGates {
		if f.Feature == feature {
			if f.Mode == operatorapiv1.FeatureGateModeTypeEnable {
				return nil
			}
			cm.Spec.RegistrationConfiguration.FeatureGates[idx].Mode = operatorapiv1.FeatureGateModeTypeEnable
			_, err = hub.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), cm, metav1.UpdateOptions{})
			return err
		}
	}

	featureGate := operatorapiv1.FeatureGate{
		Feature: feature,
		Mode:    operatorapiv1.FeatureGateModeTypeEnable,
	}

	cm.Spec.RegistrationConfiguration.FeatureGates = append(cm.Spec.RegistrationConfiguration.FeatureGates, featureGate)
	_, err = hub.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), cm, metav1.UpdateOptions{})
	return err
}

func (hub *Hub) EnableHubWorkFeature(feature string) error {
	cm, err := hub.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), hub.ClusterManagerName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if cm.Spec.WorkConfiguration == nil {
		cm.Spec.WorkConfiguration = &operatorapiv1.WorkConfiguration{}
	}

	if len(cm.Spec.WorkConfiguration.FeatureGates) == 0 {
		cm.Spec.WorkConfiguration.FeatureGates = make([]operatorapiv1.FeatureGate, 0)
	}

	for idx, f := range cm.Spec.WorkConfiguration.FeatureGates {
		if f.Feature == feature {
			if f.Mode == operatorapiv1.FeatureGateModeTypeEnable {
				return nil
			}
			cm.Spec.WorkConfiguration.FeatureGates[idx].Mode = operatorapiv1.FeatureGateModeTypeEnable
			_, err = hub.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), cm, metav1.UpdateOptions{})
			return err
		}
	}

	featureGate := operatorapiv1.FeatureGate{
		Feature: feature,
		Mode:    operatorapiv1.FeatureGateModeTypeEnable,
	}

	cm.Spec.WorkConfiguration.FeatureGates = append(cm.Spec.WorkConfiguration.FeatureGates, featureGate)
	_, err = hub.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), cm, metav1.UpdateOptions{})
	return err
}

func (hub *Hub) RemoveHubWorkFeature(feature string) error {
	clusterManager, err := hub.OperatorClient.OperatorV1().ClusterManagers().Get(context.TODO(), hub.ClusterManagerName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	for indx, fg := range clusterManager.Spec.WorkConfiguration.FeatureGates {
		if fg.Feature == feature {
			clusterManager.Spec.WorkConfiguration.FeatureGates[indx].Mode = operatorapiv1.FeatureGateModeTypeDisable
			break
		}
	}
	_, err = hub.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), clusterManager, metav1.UpdateOptions{})
	return err
}

func (hub *Hub) EnableAutoApprove(users []string) error {
	cm, err := hub.GetCluserManager()
	if err != nil {
		return fmt.Errorf("failed to get cluster manager: %w", err)
	}
	if cm.Spec.RegistrationConfiguration == nil {
		cm.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{}
	}
	cm.Spec.RegistrationConfiguration.FeatureGates = append(cm.Spec.RegistrationConfiguration.FeatureGates, operatorapiv1.FeatureGate{
		Feature: string(ocmfeature.ManagedClusterAutoApproval),
		Mode:    operatorapiv1.FeatureGateModeTypeEnable,
	})
	cm.Spec.RegistrationConfiguration.AutoApproveUsers = users
	_, err = hub.OperatorClient.OperatorV1().ClusterManagers().Update(context.TODO(), cm, metav1.UpdateOptions{})
	return err
}
