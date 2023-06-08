package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

var _ = Describe("Create klusterlet and then create a configmap by manifestwork in hosted mode", func() {
	var klusterletName, clusterName, agentNamespace, workName, configMapName string
	var configMapNamespace = "default"

	BeforeEach(func() {
		klusterletName = fmt.Sprintf("e2e-klusterlet-%s", rand.String(6))
		clusterName = fmt.Sprintf("e2e-managedcluster-%s", rand.String(6))
		agentNamespace = fmt.Sprintf("open-cluster-management-agent-%s", rand.String(6))
		workName = fmt.Sprintf("e2e-work-configmap-%s", rand.String(6))
		configMapName = fmt.Sprintf("e2e-configmap-%s", rand.String(6))

		_, err := t.CreateApprovedKlusterlet(klusterletName, clusterName, agentNamespace, operatorapiv1.InstallModeHosted)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		By(fmt.Sprintf("delete manifestwork %v/%v", clusterName, workName))
		Expect(t.cleanManifestWorks(clusterName, workName)).To(BeNil())
		By(fmt.Sprintf("clean klusterlet %s resources after the test case, clusterName: %s", klusterletName, clusterName))
		Expect(t.cleanKlusterletResources(klusterletName, clusterName)).To(BeNil())
	})

	It("Create configmap using manifestwork and then delete klusterlet in Hosted mode", func() {
		var err error

		By(fmt.Sprintf("create configmap %v/%v using manifestwork %v/%v", configMapNamespace,
			configMapName, clusterName, workName))
		_, err = t.CreateWorkOfConfigMap(workName, clusterName, configMapName, configMapNamespace)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("waiting for configmap %v/%v to be created", configMapNamespace, configMapName))
		Eventually(func() error {
			_, err := t.SpokeKubeClient.CoreV1().ConfigMaps(configMapNamespace).
				Get(context.TODO(), configMapName, metav1.GetOptions{})
			return err
		}, t.EventuallyTimeout*5, t.EventuallyInterval*5).Should(Succeed())
	})
})
