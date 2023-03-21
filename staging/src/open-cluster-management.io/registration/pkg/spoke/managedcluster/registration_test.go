package managedcluster

import (
	"testing"
	"time"

	testinghelpers "open-cluster-management.io/registration/pkg/helpers/testing"
)

func TestGetClusterAgentNamesFromCertificate(t *testing.T) {
	cases := []struct {
		name                string
		certData            []byte
		expectedClusterName string
		expectedAgentName   string
		expectedErrorPrefix string
	}{
		{
			name:                "cert data is invalid",
			certData:            []byte("invalid cert"),
			expectedErrorPrefix: "unable to parse certificate:",
		},
		{
			name:     "cert with invalid commmon name",
			certData: testinghelpers.NewTestCert("test", 60*time.Second).Cert,
		},
		{
			name:                "valid cert with correct common name",
			certData:            testinghelpers.NewTestCert("system:open-cluster-management:cluster1:agent1", 60*time.Second).Cert,
			expectedClusterName: "cluster1",
			expectedAgentName:   "agent1",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clusterName, agentName, err := GetClusterAgentNamesFromCertificate(c.certData)
			testinghelpers.AssertErrorWithPrefix(t, err, c.expectedErrorPrefix)

			if clusterName != c.expectedClusterName {
				t.Errorf("expect %v, but got %v", c.expectedClusterName, clusterName)
			}

			if agentName != c.expectedAgentName {
				t.Errorf("expect %v, but got %v", c.expectedAgentName, agentName)
			}
		})
	}
}
