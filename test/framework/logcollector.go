package framework

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

const (
	defaultLogTailLines = 1000
)

// LogCollectorConfig holds configuration for log collection
type LogCollectorConfig struct {
	OutputDir      string
	TailLines      int64
	IncludePodLogs bool
}

// DefaultLogCollectorConfig returns a default configuration for log collection
func DefaultLogCollectorConfig() *LogCollectorConfig {
	return &LogCollectorConfig{
		OutputDir:      "_artifacts",
		TailLines:      defaultLogTailLines,
		IncludePodLogs: true,
	}
}

// CollectE2ELogs collects logs from both hub and spoke clusters after test failures
func CollectE2ELogs(hub *Hub, spoke *Spoke, config *LogCollectorConfig) error {
	if config == nil {
		config = DefaultLogCollectorConfig()
	}

	// Create output directory
	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	fmt.Printf("Collecting logs to %s\n", config.OutputDir)

	// Collect hub cluster logs
	hubDir := filepath.Join(config.OutputDir, "hub")
	if err := os.MkdirAll(hubDir, 0755); err != nil {
		return fmt.Errorf("failed to create hub directory: %w", err)
	}
	if err := collectClusterLogs(hub.OCMClients, hubDir, config, "hub"); err != nil {
		fmt.Printf("Warning: failed to collect hub logs: %v\n", err)
	}

	// Collect spoke cluster logs
	spokeDir := filepath.Join(config.OutputDir, "spoke")
	if err := os.MkdirAll(spokeDir, 0755); err != nil {
		return fmt.Errorf("failed to create spoke directory: %w", err)
	}
	if err := collectClusterLogs(spoke.OCMClients, spokeDir, config, "spoke"); err != nil {
		fmt.Printf("Warning: failed to collect spoke logs: %v\n", err)
	}

	fmt.Printf("Log collection completed in %s\n", config.OutputDir)
	return nil
}

// collectClusterLogs collects logs from a single cluster
func collectClusterLogs(clients *OCMClients, outputDir string, config *LogCollectorConfig, clusterType string) error {
	ctx := context.Background()

	// Define namespaces to collect logs from
	namespaces, err := getRelevantNamespaces(ctx, clients.KubeClient)
	if err != nil {
		return fmt.Errorf("failed to get relevant namespaces: %w", err)
	}

	for _, ns := range namespaces {
		nsDir := filepath.Join(outputDir, ns)
		if err := os.MkdirAll(nsDir, 0755); err != nil {
			fmt.Printf("Warning: failed to create namespace directory %s: %v\n", nsDir, err)
			continue
		}

		// Collect pod logs and descriptions
		if config.IncludePodLogs {
			if err := collectPodLogs(ctx, clients.KubeClient, ns, nsDir, config.TailLines); err != nil {
				fmt.Printf("Warning: failed to collect pod logs for namespace %s: %v\n", ns, err)
			}
		}

		// Collect pod descriptions
		if err := collectPodDescriptions(ctx, clients.KubeClient, ns, nsDir); err != nil {
			fmt.Printf("Warning: failed to collect pod descriptions for namespace %s: %v\n", ns, err)
		}
	}

	// Collect cluster-wide resources
	if err := collectClusterResources(ctx, clients, outputDir, namespaces, clusterType); err != nil {
		fmt.Printf("Warning: failed to collect cluster resources: %v\n", err)
	}

	return nil
}

// getRelevantNamespaces returns the list of namespaces to collect logs from
// It gets all namespaces except for common system namespaces that are not relevant
func getRelevantNamespaces(ctx context.Context, client kubernetes.Interface) ([]string, error) {
	// Namespaces to exclude from log collection
	excludedNamespaces := map[string]bool{
		"kube-node-lease":    true,
		"kube-public":        true,
		"kube-system":        true,
		"local-path-storage": true,
	}

	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list namespaces: %w", err)
	}

	var relevantNamespaces []string
	for _, ns := range namespaces.Items {
		if !excludedNamespaces[ns.Name] {
			relevantNamespaces = append(relevantNamespaces, ns.Name)
		}
	}

	return relevantNamespaces, nil
}

// collectPodLogs collects logs from all pods in a namespace
func collectPodLogs(ctx context.Context, client kubernetes.Interface, namespace, outputDir string, tailLines int64) error {
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	for _, pod := range pods.Items {
		podDir := filepath.Join(outputDir, pod.Name)
		if err := os.MkdirAll(podDir, 0755); err != nil {
			fmt.Printf("Warning: failed to create pod directory %s: %v\n", podDir, err)
			continue
		}

		// Collect logs for each container
		for _, container := range pod.Spec.Containers {
			if err := collectContainerLog(ctx, client, namespace, pod.Name, container.Name, podDir, tailLines, false); err != nil {
				fmt.Printf("Warning: failed to collect logs for container %s/%s/%s: %v\n", namespace, pod.Name, container.Name, err)
			}
			// Also try to collect previous logs (will fail silently if no previous logs exist)
			if err := collectContainerLog(ctx, client, namespace, pod.Name, container.Name, podDir, tailLines, true); err == nil {
				// Previous logs exist, we collected them
			}
		}

		// Collect logs for init containers
		for _, container := range pod.Spec.InitContainers {
			if err := collectContainerLog(ctx, client, namespace, pod.Name, container.Name, podDir, tailLines, false); err != nil {
				fmt.Printf("Warning: failed to collect logs for init container %s/%s/%s: %v\n", namespace, pod.Name, container.Name, err)
			}
			// Also try to collect previous logs (will fail silently if no previous logs exist)
			if err := collectContainerLog(ctx, client, namespace, pod.Name, container.Name, podDir, tailLines, true); err == nil {
				// Previous logs exist, we collected them
			}
		}
	}

	return nil
}

// collectContainerLog collects logs from a single container
func collectContainerLog(ctx context.Context, client kubernetes.Interface, namespace, podName, containerName, outputDir string, tailLines int64, previous bool) error {
	logOptions := &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tailLines,
		Previous:  previous,
	}

	req := client.CoreV1().Pods(namespace).GetLogs(podName, logOptions)
	logs, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("failed to get log stream: %w", err)
	}
	defer logs.Close()

	filename := fmt.Sprintf("%s.log", containerName)
	if previous {
		filename = fmt.Sprintf("%s-previous.log", containerName)
	}
	filepath := filepath.Join(outputDir, filename)

	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}
	defer file.Close()

	buf := make([]byte, 2048)
	for {
		n, err := logs.Read(buf)
		if n > 0 {
			if _, writeErr := file.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("failed to write logs: %w", writeErr)
			}
		}
		if err != nil {
			break
		}
	}

	return nil
}

// collectPodDescriptions collects pod descriptions (status) for all pods
func collectPodDescriptions(ctx context.Context, client kubernetes.Interface, namespace, outputDir string) error {
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	filename := filepath.Join(outputDir, "pods.txt")
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create pods file: %w", err)
	}
	defer file.Close()

	for _, pod := range pods.Items {
		fmt.Fprintf(file, "=== Pod: %s ===\n", pod.Name)
		fmt.Fprintf(file, "Status: %s\n", pod.Status.Phase)
		fmt.Fprintf(file, "Node: %s\n", pod.Spec.NodeName)
		fmt.Fprintf(file, "Namespace: %s\n", pod.Namespace)
		fmt.Fprintf(file, "Labels: %v\n", pod.Labels)
		fmt.Fprintf(file, "\nContainer Statuses:\n")
		for _, cs := range pod.Status.ContainerStatuses {
			fmt.Fprintf(file, "  - %s: Ready=%v, RestartCount=%d, Image=%s\n",
				cs.Name, cs.Ready, cs.RestartCount, cs.Image)
			if cs.State.Waiting != nil {
				fmt.Fprintf(file, "    Waiting: %s - %s\n", cs.State.Waiting.Reason, cs.State.Waiting.Message)
			}
			if cs.State.Terminated != nil {
				fmt.Fprintf(file, "    Terminated: %s - %s (exit code: %d)\n",
					cs.State.Terminated.Reason, cs.State.Terminated.Message, cs.State.Terminated.ExitCode)
			}
		}
		fmt.Fprintf(file, "\nConditions:\n")
		for _, cond := range pod.Status.Conditions {
			fmt.Fprintf(file, "  - %s: %s (Reason: %s, Message: %s)\n",
				cond.Type, cond.Status, cond.Reason, cond.Message)
		}
		fmt.Fprintf(file, "\n")
	}

	return nil
}

// collectClusterResources collects cluster-wide resource information
func collectClusterResources(ctx context.Context, clients *OCMClients, outputDir string, relevantNamespaces []string, clusterType string) error {
	filename := filepath.Join(outputDir, "cluster-resources.txt")
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create cluster resources file: %w", err)
	}
	defer file.Close()

	// List all nodes
	nodes, err := clients.KubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err == nil {
		fmt.Fprintf(file, "=== Nodes ===\n")
		for _, node := range nodes.Items {
			fmt.Fprintf(file, "Name: %s\n", node.Name)
			fmt.Fprintf(file, "  Status: ")
			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady {
					fmt.Fprintf(file, "%s", cond.Status)
					break
				}
			}
			fmt.Fprintf(file, "\n  Version: %s\n", node.Status.NodeInfo.KubeletVersion)
			fmt.Fprintf(file, "  OS: %s\n", node.Status.NodeInfo.OSImage)
			fmt.Fprintf(file, "\n")
		}
	}

	// List all namespaces
	namespaces, err := clients.KubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err == nil {
		fmt.Fprintf(file, "\n=== Namespaces ===\n")
		for _, ns := range namespaces.Items {
			fmt.Fprintf(file, "- %s (Status: %s)\n", ns.Name, ns.Status.Phase)
		}
	}

	// List deployments in relevant namespaces
	for _, ns := range relevantNamespaces {
		deployments, err := clients.KubeClient.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}

		if len(deployments.Items) > 0 {
			fmt.Fprintf(file, "\n=== Deployments in %s ===\n", ns)
			for _, deploy := range deployments.Items {
				fmt.Fprintf(file, "- %s: %d/%d replicas ready\n",
					deploy.Name,
					deploy.Status.ReadyReplicas,
					deploy.Status.Replicas)
			}
		}
	}

	// Collect OCM custom resources
	if err := collectOCMCustomResources(ctx, clients, file, clusterType); err != nil {
		fmt.Printf("Warning: failed to collect OCM custom resources: %v\n", err)
	}

	return nil
}

// writeResourceAsYAML writes a Kubernetes resource object as YAML to a file
func writeResourceAsYAML(file *os.File, resource runtime.Object, name string) error {
	yamlBytes, err := yaml.Marshal(resource)
	if err != nil {
		return fmt.Errorf("failed to marshal %s to YAML: %w", name, err)
	}
	fmt.Fprintf(file, "\n---\n# %s\n", name)
	if _, err := file.Write(yamlBytes); err != nil {
		return fmt.Errorf("failed to write %s YAML: %w", name, err)
	}
	return nil
}

// collectOCMCustomResources collects OCM-specific custom resources
func collectOCMCustomResources(ctx context.Context, clients *OCMClients, file *os.File, clusterType string) error {
	if clusterType == "hub" {
		// Collect hub-specific CRs

		// ClusterManager
		clusterManagers, err := clients.OperatorClient.OperatorV1().ClusterManagers().List(ctx, metav1.ListOptions{})
		if err == nil && len(clusterManagers.Items) > 0 {
			fmt.Fprintf(file, "\n=== ClusterManagers ===\n")
			for _, cm := range clusterManagers.Items {
				if err := writeResourceAsYAML(file, &cm, fmt.Sprintf("ClusterManager/%s", cm.Name)); err != nil {
					fmt.Printf("Warning: failed to write ClusterManager %s: %v\n", cm.Name, err)
				}
			}
		}

		// ManagedClusters
		managedClusters, err := clients.ClusterClient.ClusterV1().ManagedClusters().List(ctx, metav1.ListOptions{})
		if err == nil && len(managedClusters.Items) > 0 {
			fmt.Fprintf(file, "\n=== ManagedClusters ===\n")
			for _, mc := range managedClusters.Items {
				if err := writeResourceAsYAML(file, &mc, fmt.Sprintf("ManagedCluster/%s", mc.Name)); err != nil {
					fmt.Printf("Warning: failed to write ManagedCluster %s: %v\n", mc.Name, err)
				}
			}
		}

		// ManagedClusterSets
		managedClusterSets, err := clients.ClusterClient.ClusterV1beta2().ManagedClusterSets().List(ctx, metav1.ListOptions{})
		if err == nil && len(managedClusterSets.Items) > 0 {
			fmt.Fprintf(file, "\n=== ManagedClusterSets ===\n")
			for _, mcs := range managedClusterSets.Items {
				if err := writeResourceAsYAML(file, &mcs, fmt.Sprintf("ManagedClusterSet/%s", mcs.Name)); err != nil {
					fmt.Printf("Warning: failed to write ManagedClusterSet %s: %v\n", mcs.Name, err)
				}
			}
		}

		// Placements
		placements, err := clients.ClusterClient.ClusterV1beta1().Placements("").List(ctx, metav1.ListOptions{})
		if err == nil && len(placements.Items) > 0 {
			fmt.Fprintf(file, "\n=== Placements ===\n")
			for _, p := range placements.Items {
				if err := writeResourceAsYAML(file, &p, fmt.Sprintf("Placement/%s/%s", p.Namespace, p.Name)); err != nil {
					fmt.Printf("Warning: failed to write Placement %s/%s: %v\n", p.Namespace, p.Name, err)
				}
			}
		}

		// PlacementDecisions
		placementDecisions, err := clients.ClusterClient.ClusterV1beta1().PlacementDecisions("").List(ctx, metav1.ListOptions{})
		if err == nil && len(placementDecisions.Items) > 0 {
			fmt.Fprintf(file, "\n=== PlacementDecisions ===\n")
			for _, pd := range placementDecisions.Items {
				if err := writeResourceAsYAML(file, &pd, fmt.Sprintf("PlacementDecision/%s/%s", pd.Namespace, pd.Name)); err != nil {
					fmt.Printf("Warning: failed to write PlacementDecision %s/%s: %v\n", pd.Namespace, pd.Name, err)
				}
			}
		}

		// ClusterManagementAddons (v1alpha1 only)
		clusterMgmtAddons, err := clients.AddonClient.AddonV1alpha1().ClusterManagementAddOns().List(ctx, metav1.ListOptions{})
		if err == nil && len(clusterMgmtAddons.Items) > 0 {
			fmt.Fprintf(file, "\n=== ClusterManagementAddons (v1alpha1) ===\n")
			for _, cma := range clusterMgmtAddons.Items {
				if err := writeResourceAsYAML(file, &cma, fmt.Sprintf("ClusterManagementAddOn/%s", cma.Name)); err != nil {
					fmt.Printf("Warning: failed to write ClusterManagementAddOn %s: %v\n", cma.Name, err)
				}
			}
		}

		// ManagedClusterAddons (v1alpha1 only)
		managedClusterAddons, err := clients.AddonClient.AddonV1alpha1().ManagedClusterAddOns("").List(ctx, metav1.ListOptions{})
		if err == nil && len(managedClusterAddons.Items) > 0 {
			fmt.Fprintf(file, "\n=== ManagedClusterAddons (v1alpha1) ===\n")
			for _, mca := range managedClusterAddons.Items {
				if err := writeResourceAsYAML(file, &mca, fmt.Sprintf("ManagedClusterAddOn/%s/%s", mca.Namespace, mca.Name)); err != nil {
					fmt.Printf("Warning: failed to write ManagedClusterAddOn %s/%s: %v\n", mca.Namespace, mca.Name, err)
				}
			}
		}

		// AddonDeploymentConfigs (v1alpha1 only)
		addonDeploymentConfigs, err := clients.AddonClient.AddonV1alpha1().AddOnDeploymentConfigs("").List(ctx, metav1.ListOptions{})
		if err == nil && len(addonDeploymentConfigs.Items) > 0 {
			fmt.Fprintf(file, "\n=== AddonDeploymentConfigs (v1alpha1) ===\n")
			for _, adc := range addonDeploymentConfigs.Items {
				if err := writeResourceAsYAML(file, &adc, fmt.Sprintf("AddOnDeploymentConfig/%s/%s", adc.Namespace, adc.Name)); err != nil {
					fmt.Printf("Warning: failed to write AddOnDeploymentConfig %s/%s: %v\n", adc.Namespace, adc.Name, err)
				}
			}
		}

		// AddonTemplates (v1alpha1 only)
		addonTemplates, err := clients.AddonClient.AddonV1alpha1().AddOnTemplates().List(ctx, metav1.ListOptions{})
		if err == nil && len(addonTemplates.Items) > 0 {
			fmt.Fprintf(file, "\n=== AddonTemplates (v1alpha1) ===\n")
			for _, at := range addonTemplates.Items {
				if err := writeResourceAsYAML(file, &at, fmt.Sprintf("AddOnTemplate/%s", at.Name)); err != nil {
					fmt.Printf("Warning: failed to write AddOnTemplate %s: %v\n", at.Name, err)
				}
			}
		}

		// ManifestWorks
		manifestWorks, err := clients.WorkClient.WorkV1().ManifestWorks("").List(ctx, metav1.ListOptions{})
		if err == nil && len(manifestWorks.Items) > 0 {
			fmt.Fprintf(file, "\n=== ManifestWorks ===\n")
			for _, mw := range manifestWorks.Items {
				if err := writeResourceAsYAML(file, &mw, fmt.Sprintf("ManifestWork/%s/%s", mw.Namespace, mw.Name)); err != nil {
					fmt.Printf("Warning: failed to write ManifestWork %s/%s: %v\n", mw.Namespace, mw.Name, err)
				}
			}
		}

		// ManifestWorkReplicaSets
		manifestWorkReplicaSets, err := clients.WorkClient.WorkV1alpha1().ManifestWorkReplicaSets("").List(ctx, metav1.ListOptions{})
		if err == nil && len(manifestWorkReplicaSets.Items) > 0 {
			fmt.Fprintf(file, "\n=== ManifestWorkReplicaSets ===\n")
			for _, mwrs := range manifestWorkReplicaSets.Items {
				if err := writeResourceAsYAML(file, &mwrs, fmt.Sprintf("ManifestWorkReplicaSet/%s/%s", mwrs.Namespace, mwrs.Name)); err != nil {
					fmt.Printf("Warning: failed to write ManifestWorkReplicaSet %s/%s: %v\n", mwrs.Namespace, mwrs.Name, err)
				}
			}
		}
	} else {
		// Collect spoke-specific CRs

		// Klusterlet
		klusterlets, err := clients.OperatorClient.OperatorV1().Klusterlets().List(ctx, metav1.ListOptions{})
		if err == nil && len(klusterlets.Items) > 0 {
			fmt.Fprintf(file, "\n=== Klusterlets ===\n")
			for _, kl := range klusterlets.Items {
				if err := writeResourceAsYAML(file, &kl, fmt.Sprintf("Klusterlet/%s", kl.Name)); err != nil {
					fmt.Printf("Warning: failed to write Klusterlet %s: %v\n", kl.Name, err)
				}
			}
		}

		// AppliedManifestWorks
		appliedManifestWorks, err := clients.WorkClient.WorkV1().AppliedManifestWorks().List(ctx, metav1.ListOptions{})
		if err == nil && len(appliedManifestWorks.Items) > 0 {
			fmt.Fprintf(file, "\n=== AppliedManifestWorks ===\n")
			for _, amw := range appliedManifestWorks.Items {
				if err := writeResourceAsYAML(file, &amw, fmt.Sprintf("AppliedManifestWork/%s", amw.Name)); err != nil {
					fmt.Printf("Warning: failed to write AppliedManifestWork %s: %v\n", amw.Name, err)
				}
			}
		}

		// ClusterClaims
		clusterClaims, err := clients.ClusterClient.ClusterV1alpha1().ClusterClaims().List(ctx, metav1.ListOptions{})
		if err == nil && len(clusterClaims.Items) > 0 {
			fmt.Fprintf(file, "\n=== ClusterClaims ===\n")
			for _, cc := range clusterClaims.Items {
				if err := writeResourceAsYAML(file, &cc, fmt.Sprintf("ClusterClaim/%s", cc.Name)); err != nil {
					fmt.Printf("Warning: failed to write ClusterClaim %s: %v\n", cc.Name, err)
				}
			}
		}
	}

	return nil
}
