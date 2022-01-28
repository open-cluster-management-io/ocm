package addonfactory

import (
	"embed"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1apha1 "open-cluster-management.io/api/cluster/v1alpha1"
)

//go:embed testmanifests
var templateFS embed.FS

func TestTemplateAddon_Manifests(t *testing.T) {
	type config struct {
		NodeSelector map[string]string
		Image        string
	}

	scheme := runtime.NewScheme()
	_ = clusterv1apha1.Install(scheme)

	cases := []struct {
		name                     string
		dir                      string
		scheme                   *runtime.Scheme
		clusterName              string
		addonName                string
		installNamespace         string
		getValuesFunc            GetValuesFunc
		annotationConfig         string
		expectedInstallNamespace string
		expectedNodeSelector     map[string]string
		expectedImage            string
		expectedObjectCnt        int
	}{
		{
			name:             "template render ok with annotation config and default scheme",
			dir:              "testmanifests/template",
			clusterName:      "cluster1",
			addonName:        "helloworld",
			installNamespace: "myNs",
			scheme:           scheme,
			getValuesFunc: func(cluster *clusterv1.ManagedCluster,
				addon *addonapiv1alpha1.ManagedClusterAddOn) (Values, error) {
				config := config{Image: "quay.io/helloworld:latest"}
				return StructToValues(config), nil
			},
			annotationConfig:         `{"NodeSelector":{"host":"ssd"},"Image":"quay.io/helloworld:2.4"}`,
			expectedInstallNamespace: "myNs",
			expectedNodeSelector:     map[string]string{"host": "ssd"},
			expectedImage:            "quay.io/helloworld:2.4",
			expectedObjectCnt:        2,
		},
		{
			name:        "deployment template render ok with default scheme but no annotation config",
			dir:         "testmanifests/template",
			clusterName: "cluster1",
			addonName:   "helloworld",
			scheme:      scheme,
			getValuesFunc: func(cluster *clusterv1.ManagedCluster,
				addon *addonapiv1alpha1.ManagedClusterAddOn) (Values, error) {
				config := config{Image: "quay.io/helloworld:latest"}
				return StructToValues(config), nil
			},
			expectedInstallNamespace: AddonDefaultInstallNamespace,
			expectedNodeSelector:     map[string]string{},
			expectedImage:            "quay.io/helloworld:latest",
			expectedObjectCnt:        2,
		},
		{
			name:                     "deployment template render ok with default scheme,but no userConfig",
			dir:                      "testmanifests/template",
			clusterName:              "cluster1",
			addonName:                "helloworld",
			scheme:                   scheme,
			annotationConfig:         `{"NodeSelector":{"host":"ssd"},"Image":"quay.io/helloworld:2.4"}`,
			expectedInstallNamespace: AddonDefaultInstallNamespace,
			expectedNodeSelector:     map[string]string{"host": "ssd"},
			expectedImage:            "quay.io/helloworld:2.4",
			expectedObjectCnt:        2,
		},
		{
			name:                     "template render ok with userConfig and custom scheme",
			dir:                      "testmanifests/template",
			scheme:                   scheme,
			clusterName:              "cluster1",
			addonName:                "helloworld",
			annotationConfig:         `{"NodeSelector":{"host":"ssd"},"Image":"quay.io/helloworld:2.4"}`,
			expectedInstallNamespace: AddonDefaultInstallNamespace,
			expectedNodeSelector:     map[string]string{"host": "ssd"},
			expectedImage:            "quay.io/helloworld:2.4",
			expectedObjectCnt:        2,
		},
		{
			name:                     "template render ok with empty yaml",
			dir:                      "testmanifests/template",
			scheme:                   scheme,
			clusterName:              "local-cluster",
			addonName:                "helloworld",
			annotationConfig:         `{"NodeSelector":{"host":"ssd"},"Image":"quay.io/helloworld:2.4"}`,
			expectedInstallNamespace: AddonDefaultInstallNamespace,
			expectedNodeSelector:     map[string]string{"host": "ssd"},
			expectedImage:            "quay.io/helloworld:2.4",
			expectedObjectCnt:        1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cluster := NewFakeManagedCluster(c.clusterName)
			clusterAddon := NewFakeManagedClusterAddon(c.addonName, c.clusterName, c.installNamespace,
				c.annotationConfig)

			agentAddon, err := NewAgentAddonFactory(c.addonName, templateFS, c.dir).
				WithScheme(c.scheme).
				WithGetValuesFuncs(c.getValuesFunc, GetValuesFromAddonAnnotation).
				BuildTemplateAgentAddon()
			if err != nil {
				t.Errorf("expected no error, got err %v", err)
			}
			objects, err := agentAddon.Manifests(cluster, clusterAddon)
			if err != nil {
				t.Errorf("expected no error, got err %v", err)
			}
			if len(objects) != c.expectedObjectCnt {
				t.Errorf("expected %v objects, but got %v", c.expectedObjectCnt, len(objects))
			}
			for _, o := range objects {
				switch object := o.(type) {
				case *appsv1.Deployment:
					if object.Namespace != c.expectedInstallNamespace {
						t.Errorf("expected namespace is %s, but got %s", c.expectedInstallNamespace, object.Namespace)
					}

					labels := object.GetLabels()
					if labels["clusterName"] != c.clusterName {
						t.Errorf("expected label is %s, but got %s", c.clusterName, labels["clusterName"])
					}

					nodeSelector := object.Spec.Template.Spec.NodeSelector
					for k, v := range c.expectedNodeSelector {
						if nodeSelector[k] != v {
							t.Errorf("expected nodeSelector is %v, but got %v", c.expectedNodeSelector, nodeSelector)
						}
					}

					if object.Spec.Template.Spec.Containers[0].Image != c.expectedImage {
						t.Errorf("expected image is %s, but got %s", c.expectedImage, object.Spec.Template.Spec.Containers[0].Image)
					}
				case *clusterv1apha1.ClusterClaim:
					if object.GetName() != c.expectedInstallNamespace {
						t.Errorf("expected name is %s, but got %s", c.expectedInstallNamespace, object.GetName())
					}
					if object.Spec.Value != c.expectedImage {
						t.Errorf("expected image is %s, but got %s", c.expectedImage, object.Spec.Value)
					}
				}
			}
		})
	}
}
