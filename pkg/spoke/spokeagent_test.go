package spoke

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/open-cluster-management/registration/pkg/spoke/hubclientcert"
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
