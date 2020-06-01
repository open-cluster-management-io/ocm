package spoke

import (
	"bytes"
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	"github.com/open-cluster-management/registration/pkg/spoke/hubclientcert"
	"k8s.io/client-go/rest"
)

func TestGetOrGenerateClusterAgentNames(t *testing.T) {
	o := &SpokeAgentOptions{
		HubKubeconfigDir: "/path/not/existing",
	}

	clusterName, agentName := o.getOrGenerateClusterAgentNames()
	if clusterName == "" {
		t.Error("cluster name should not be empty")
	}

	if agentName == "" {
		t.Error("agent name should not be empty")
	}
}

func TestGetOrGenerateClusterAgentNamesWithClusterNameOverride(t *testing.T) {
	dir, err := ioutil.TempDir("", "prefix")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer os.RemoveAll(dir)

	o := &SpokeAgentOptions{
		HubKubeconfigDir: "/path/not/existing",
		ClusterName:      "cluster0",
	}

	clusterName, agentName := o.getOrGenerateClusterAgentNames()

	if clusterName != o.ClusterName {
		t.Errorf("expect cluster name %q but got %q", o.ClusterName, clusterName)
	}

	if agentName == "" {
		t.Error("agent name should not be empty")
	}
}

func TestGetOrGenerateClusterAgentNamesWithExistingNames(t *testing.T) {
	dir, err := ioutil.TempDir("", "prefix")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	defer os.RemoveAll(dir)

	cn, an := "cluster0", "agent0"

	clusterNameFilePath := path.Join(dir, hubclientcert.ClusterNameFile)
	err = ioutil.WriteFile(clusterNameFilePath, []byte(cn), 0644)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	agentNameFilePath := path.Join(dir, hubclientcert.AgentNameFile)
	err = ioutil.WriteFile(agentNameFilePath, []byte(an), 0644)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	o := &SpokeAgentOptions{
		HubKubeconfigDir: dir,
	}

	clusterName, agentName := o.getOrGenerateClusterAgentNames()

	if clusterName != cn {
		t.Errorf("expect cluster name %q but got %q", cn, clusterName)
	}

	if agentName != an {
		t.Errorf("expect agent name %q but got %q", an, agentName)
	}
}

func TestValidate(t *testing.T) {
	var err error

	withoutBootstrapKubeconfig := &SpokeAgentOptions{}
	err = withoutBootstrapKubeconfig.Validate()
	if err == nil || err.Error() != "bootstrap-kubeconfig is required" {
		t.Errorf("expect 'bootstrap-kubeconfig is required' error but got %v", err)
	}

	withoutClusterName := &SpokeAgentOptions{
		BootstrapKubeconfig: "/spoke/bootstrap/kubeconfig",
	}
	err = withoutClusterName.Validate()
	if err == nil || err.Error() != "cluster name is empty" {
		t.Errorf("expect \"cluster name is empty\" error but got %v", err)
	}

	withoutAgentName := &SpokeAgentOptions{
		BootstrapKubeconfig: "/spoke/bootstrap/kubeconfig",
		ClusterName:         "testcluster",
	}
	err = withoutAgentName.Validate()
	if err == nil || err.Error() != "agent name is empty" {
		t.Errorf("expect \"agent name is empty\" error but got %v", err)
	}

	withoutSpokeExternalServerURLs := &SpokeAgentOptions{
		BootstrapKubeconfig:      "/spoke/bootstrap/kubeconfig",
		ClusterName:              "testcluster",
		AgentName:                "testagent",
		ClusterHealthCheckPeriod: 1 * time.Minute,
	}
	err = withoutSpokeExternalServerURLs.Validate()
	if err != nil {
		t.Errorf("expect no error but got %v", err)
	}

	withInvalidSpokeExternalServerURL := &SpokeAgentOptions{
		BootstrapKubeconfig:     "/spoke/bootstrap/kubeconfig",
		ClusterName:             "testcluster",
		AgentName:               "testagent",
		SpokeExternalServerURLs: []string{"https://127.0.0.1:64433", "http://127.0.0.1:8080"},
	}
	err = withInvalidSpokeExternalServerURL.Validate()
	if err == nil || err.Error() != "\"http://127.0.0.1:8080\" is invalid" {
		t.Errorf("expect \"http://127.0.0.1:8080 is invalid\" error but got %v", err)
	}

	withInvalidClusterHealthCheckPeriod := &SpokeAgentOptions{
		BootstrapKubeconfig:      "/spoke/bootstrap/kubeconfig",
		ClusterName:              "testcluster",
		AgentName:                "testagent",
		ClusterHealthCheckPeriod: 0,
	}
	err = withInvalidClusterHealthCheckPeriod.Validate()
	if err == nil || err.Error() != "cluster healthcheck period must greater than zero" {
		t.Errorf("expect no error but got %v", err)
	}
}

func TestGetSpokeClusterCABundle(t *testing.T) {
	withoutSpokeExternalServerURLs := &SpokeAgentOptions{}
	caData, err := withoutSpokeExternalServerURLs.getSpokeClusterCABundle(&rest.Config{})
	if err != nil {
		t.Errorf("expect no error but got %v", err)
	}
	if caData != nil {
		t.Errorf("expect no ca data but got %v", caData)
	}

	withSpokeExternalServerURLs := &SpokeAgentOptions{SpokeExternalServerURLs: []string{"https://127.0.0.1:6443"}}
	caData, err = withSpokeExternalServerURLs.getSpokeClusterCABundle(&rest.Config{})
	if err == nil {
		t.Errorf("expect error happened but no error")
	}
	if caData != nil {
		t.Errorf("expect no ca data but got %v", caData)
	}

	expectedCAData := []byte("cadata")
	caData, err = withSpokeExternalServerURLs.getSpokeClusterCABundle(&rest.Config{
		TLSClientConfig: rest.TLSClientConfig{CAData: expectedCAData},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !bytes.Equal(caData, expectedCAData) {
		t.Errorf("expected %v but got %v", expectedCAData, caData)
	}
}
