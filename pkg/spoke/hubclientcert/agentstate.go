package hubclientcert

import (
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog"
)

// RecoverAgentState recovers the current state of the spoke cluster agent.
func RecoverAgentState(componentNamesapce, hubKubeconfigSecret, clusterName string, spokeCoreClient corev1client.CoreV1Interface) (string, string, bool, error) {
	kubeconfigSecretStore := NewSecretStore(componentNamesapce, hubKubeconfigSecret, spokeCoreClient)

	secretData, err := kubeconfigSecretStore.GetData()
	if err != nil {
		return "", "", false, err
	}

	var agentName string
	certData, ok := secretData[hubClientCertFile]
	if ok {
		valid, err := isClientCertificateStillValid(certData, clusterName)
		if err != nil {
			return "", "", false, nil
		}

		if valid {
			klog.Info("Client certificate for hub exists and is valid, skipping bootstrap")
			clusterName, agentName, err = getClusterAgentNamesFromCertificates(certData)
			if err != nil {
				return "", "", false, err
			}

			klog.V(4).Infof("Agent is recoved from client certifiate for hub: %s, %s", clusterName, agentName)
			return clusterName, agentName, true, nil
		}
		klog.Info("Client certificate for hub is no long valid, bootstrap is required")
	} else {
		klog.Info("No client certificate for hub is found, bootstrap is required")
	}

	// reuse the cluster names saved in secrec if there is no cluster name specified
	if clusterName == "" {
		if value, ok := secretData[clusterNameFile]; ok {
			clusterName = string(value)
		} else {
			// generate cluster name if it is empty.
			clusterName, err = generateClusterName()
			if err != nil {
				return "", "", false, err
			}
			secretData[clusterNameFile] = []byte(clusterName)
			klog.V(4).Infof("Cluster name generated: %s", clusterName)
		}
	}

	// generate agent name if it is empty.
	if value, ok := secretData[agentNameFile]; ok {
		agentName = string(value)
	} else {
		agentName, err = generateAgentName()
		if err != nil {
			return "", "", false, err
		}
		secretData[agentNameFile] = []byte(agentName)
		klog.V(4).Infof("Agent name generated: %s", agentName)
	}

	// save the latest cluster/agent names in secret, and they can be
	// reused once spoke agent is restared
	_, err = kubeconfigSecretStore.SetData(secretData)
	if err != nil {
		return "", "", false, err
	}

	return clusterName, agentName, false, nil
}

// generateClusterName generates a random name for cluster or return cluster UID if it's an openshift cluster
func generateClusterName() (string, error) {
	// TODO add logic to generate random cluster name
	return "cluster0", nil
}

// generateAgentName generates a random name for cluster agent
func generateAgentName() (string, error) {
	// TODO add logic to generate random agent name
	return "agent0", nil
}
