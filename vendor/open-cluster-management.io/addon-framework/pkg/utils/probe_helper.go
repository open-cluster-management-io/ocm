package utils

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/addon-framework/pkg/agent"
)

// DeploymentProber is to check the addon status based on status
// of the agent deployment status
type DeploymentProber struct {
	deployments []types.NamespacedName
}

func NewDeploymentProber(deployments ...types.NamespacedName) *agent.HealthProber {
	probeFields := []agent.ProbeField{}
	for _, deploy := range deployments {
		mc := DeploymentWellKnowManifestConfig(deploy.Namespace, deploy.Name)
		probeFields = append(probeFields, agent.ProbeField{
			ResourceIdentifier: mc.ResourceIdentifier,
			ProbeRules:         mc.FeedbackRules,
		})
	}
	return &agent.HealthProber{
		Type: agent.HealthProberTypeWork,
		WorkProber: &agent.WorkHealthProber{
			ProbeFields: probeFields,
			HealthCheck: DeploymentAvailabilityHealthCheck,
		},
	}
}

func (d *DeploymentProber) ProbeFields() []agent.ProbeField {
	probeFields := []agent.ProbeField{}
	for _, deploy := range d.deployments {
		probeFields = append(probeFields, agent.ProbeField{
			ResourceIdentifier: workapiv1.ResourceIdentifier{
				Group:     "apps",
				Resource:  "deployments",
				Name:      deploy.Name,
				Namespace: deploy.Namespace,
			},
			ProbeRules: []workapiv1.FeedbackRule{
				{
					Type: workapiv1.WellKnownStatusType,
				},
			},
		})
	}
	return probeFields
}

func DeploymentAvailabilityHealthCheck(identifier workapiv1.ResourceIdentifier, result workapiv1.StatusFeedbackResult) error {
	if identifier.Resource != "deployments" {
		return fmt.Errorf("unsupported resource type %s", identifier.Resource)
	}
	if identifier.Group != "apps" {
		return fmt.Errorf("unsupported resource group %s", identifier.Group)
	}

	if len(result.Values) == 0 {
		return fmt.Errorf("no values are probed for deployment %s/%s", identifier.Namespace, identifier.Name)
	}
	for _, value := range result.Values {
		if value.Name != "ReadyReplicas" {
			continue
		}

		if *value.Value.Integer >= 1 {
			return nil
		}

		return fmt.Errorf("readyReplica is %d for deployment %s/%s",
			*value.Value.Integer, identifier.Namespace, identifier.Name)
	}
	return fmt.Errorf("readyReplica is not probed")
}

func FilterDeployments(objects []runtime.Object) []*appsv1.Deployment {
	deployments := []*appsv1.Deployment{}
	for _, obj := range objects {
		deployment, err := ConvertToDeployment(obj)
		if err != nil {
			continue
		}
		deployments = append(deployments, deployment)
	}
	return deployments
}

func ConvertToDeployment(obj runtime.Object) (*appsv1.Deployment, error) {
	if deployment, ok := obj.(*appsv1.Deployment); ok {
		return deployment, nil
	}

	if obj.GetObjectKind().GroupVersionKind().Group != "apps" ||
		obj.GetObjectKind().GroupVersionKind().Kind != "Deployment" {
		return nil, fmt.Errorf("not deployment object, %v", obj.GetObjectKind())
	}

	deployment := &appsv1.Deployment{}
	uobj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return deployment, fmt.Errorf("not unstructured object, %v", obj.GetObjectKind())
	}

	err := runtime.DefaultUnstructuredConverter.FromUnstructured(uobj.Object, deployment)
	if err != nil {
		return nil, err
	}
	return deployment, nil
}

func DeploymentWellKnowManifestConfig(namespace, name string) workapiv1.ManifestConfigOption {
	return workapiv1.ManifestConfigOption{
		ResourceIdentifier: workapiv1.ResourceIdentifier{
			Group:     "apps",
			Resource:  "deployments",
			Name:      name,
			Namespace: namespace,
		},
		FeedbackRules: []workapiv1.FeedbackRule{
			{
				Type: workapiv1.WellKnownStatusType,
			},
		},
	}
}
