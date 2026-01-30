package framework

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	defaultLogTailLines = 1000
)

// LogCollectorConfig holds configuration for log collection
type LogCollectorConfig struct {
	OutputDir      string
	TailLines      int64
	IncludeEvents  bool
	IncludePodLogs bool
}

// DefaultLogCollectorConfig returns a default configuration for log collection
func DefaultLogCollectorConfig() *LogCollectorConfig {
	return &LogCollectorConfig{
		OutputDir:      "_artifacts",
		TailLines:      defaultLogTailLines,
		IncludeEvents:  true,
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

		// Collect events
		if config.IncludeEvents {
			if err := collectEvents(ctx, clients.KubeClient, ns, nsDir); err != nil {
				fmt.Printf("Warning: failed to collect events for namespace %s: %v\n", ns, err)
			}
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
		}

		// Collect logs for init containers
		for _, container := range pod.Spec.InitContainers {
			if err := collectContainerLog(ctx, client, namespace, pod.Name, container.Name, podDir, tailLines, false); err != nil {
				fmt.Printf("Warning: failed to collect logs for init container %s/%s/%s: %v\n", namespace, pod.Name, container.Name, err)
			}
		}

		// Also collect previous container logs if pod has restarted
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if containerStatus.RestartCount > 0 {
				if err := collectContainerLog(ctx, client, namespace, pod.Name, containerStatus.Name, podDir, tailLines, true); err != nil {
					fmt.Printf("Warning: failed to collect previous logs for container %s/%s/%s: %v\n", namespace, pod.Name, containerStatus.Name, err)
				}
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

// collectEvents collects events from a namespace
func collectEvents(ctx context.Context, client kubernetes.Interface, namespace, outputDir string) error {
	events, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list events: %w", err)
	}

	filename := filepath.Join(outputDir, "events.txt")
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create events file: %w", err)
	}
	defer file.Close()

	fmt.Fprintf(file, "=== Events in namespace %s ===\n\n", namespace)
	for _, event := range events.Items {
		fmt.Fprintf(file, "[%s] %s - %s/%s: %s\n",
			event.Type,
			event.LastTimestamp.Format("2006-01-02 15:04:05"),
			event.InvolvedObject.Kind,
			event.InvolvedObject.Name,
			event.Message)
		if event.Reason != "" {
			fmt.Fprintf(file, "  Reason: %s\n", event.Reason)
		}
		if event.Count > 1 {
			fmt.Fprintf(file, "  Count: %d\n", event.Count)
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

// collectOCMCustomResources collects OCM-specific custom resources
func collectOCMCustomResources(ctx context.Context, clients *OCMClients, file *os.File, clusterType string) error {
	if clusterType == "hub" {
		// Collect hub-specific CRs

		// ClusterManager
		clusterManagers, err := clients.OperatorClient.OperatorV1().ClusterManagers().List(ctx, metav1.ListOptions{})
		if err == nil && len(clusterManagers.Items) > 0 {
			fmt.Fprintf(file, "\n=== ClusterManagers ===\n")
			for _, cm := range clusterManagers.Items {
				fmt.Fprintf(file, "- %s (DeployOption: %s)\n",
					cm.Name,
					cm.Spec.DeployOption.Mode)
			}
		}

		// ManagedClusters
		managedClusters, err := clients.ClusterClient.ClusterV1().ManagedClusters().List(ctx, metav1.ListOptions{})
		if err == nil && len(managedClusters.Items) > 0 {
			fmt.Fprintf(file, "\n=== ManagedClusters ===\n")
			for _, mc := range managedClusters.Items {
				fmt.Fprintf(file, "- %s (Accepted: %v, Available: %v)\n",
					mc.Name,
					mc.Spec.HubAcceptsClient,
					len(mc.Status.Conditions) > 0)
			}
		}

		// ManagedClusterSets
		managedClusterSets, err := clients.ClusterClient.ClusterV1beta2().ManagedClusterSets().List(ctx, metav1.ListOptions{})
		if err == nil && len(managedClusterSets.Items) > 0 {
			fmt.Fprintf(file, "\n=== ManagedClusterSets ===\n")
			for _, mcs := range managedClusterSets.Items {
				fmt.Fprintf(file, "- %s\n", mcs.Name)
			}
		}

		// Placements
		placements, err := clients.ClusterClient.ClusterV1beta1().Placements("").List(ctx, metav1.ListOptions{})
		if err == nil && len(placements.Items) > 0 {
			fmt.Fprintf(file, "\n=== Placements ===\n")
			for _, p := range placements.Items {
				fmt.Fprintf(file, "- %s/%s (NumberOfSelectedClusters: %d)\n",
					p.Namespace, p.Name, p.Status.NumberOfSelectedClusters)
			}
		}

		// PlacementDecisions
		placementDecisions, err := clients.ClusterClient.ClusterV1beta1().PlacementDecisions("").List(ctx, metav1.ListOptions{})
		if err == nil && len(placementDecisions.Items) > 0 {
			fmt.Fprintf(file, "\n=== PlacementDecisions ===\n")
			for _, pd := range placementDecisions.Items {
				fmt.Fprintf(file, "- %s/%s (Decisions: %d)\n",
					pd.Namespace, pd.Name, len(pd.Status.Decisions))
			}
		}

		// ClusterManagementAddons (v1alpha1 only)
		clusterMgmtAddons, err := clients.AddonClient.AddonV1alpha1().ClusterManagementAddOns().List(ctx, metav1.ListOptions{})
		if err == nil && len(clusterMgmtAddons.Items) > 0 {
			fmt.Fprintf(file, "\n=== ClusterManagementAddons (v1alpha1) ===\n")
			for _, cma := range clusterMgmtAddons.Items {
				fmt.Fprintf(file, "- %s\n", cma.Name)
			}
		}

		// ManagedClusterAddons (v1alpha1 only)
		managedClusterAddons, err := clients.AddonClient.AddonV1alpha1().ManagedClusterAddOns("").List(ctx, metav1.ListOptions{})
		if err == nil && len(managedClusterAddons.Items) > 0 {
			fmt.Fprintf(file, "\n=== ManagedClusterAddons (v1alpha1) ===\n")
			for _, mca := range managedClusterAddons.Items {
				fmt.Fprintf(file, "- %s/%s\n", mca.Namespace, mca.Name)
			}
		}

		// AddonDeploymentConfigs (v1alpha1 only)
		addonDeploymentConfigs, err := clients.AddonClient.AddonV1alpha1().AddOnDeploymentConfigs("").List(ctx, metav1.ListOptions{})
		if err == nil && len(addonDeploymentConfigs.Items) > 0 {
			fmt.Fprintf(file, "\n=== AddonDeploymentConfigs (v1alpha1) ===\n")
			for _, adc := range addonDeploymentConfigs.Items {
				fmt.Fprintf(file, "- %s/%s\n", adc.Namespace, adc.Name)
			}
		}

		// AddonTemplates (v1alpha1 only)
		addonTemplates, err := clients.AddonClient.AddonV1alpha1().AddOnTemplates().List(ctx, metav1.ListOptions{})
		if err == nil && len(addonTemplates.Items) > 0 {
			fmt.Fprintf(file, "\n=== AddonTemplates (v1alpha1) ===\n")
			for _, at := range addonTemplates.Items {
				fmt.Fprintf(file, "- %s\n", at.Name)
			}
		}

		// ManifestWorks
		manifestWorks, err := clients.WorkClient.WorkV1().ManifestWorks("").List(ctx, metav1.ListOptions{})
		if err == nil && len(manifestWorks.Items) > 0 {
			fmt.Fprintf(file, "\n=== ManifestWorks ===\n")
			for _, mw := range manifestWorks.Items {
				fmt.Fprintf(file, "- %s/%s\n", mw.Namespace, mw.Name)
			}
		}

		// ManifestWorkReplicaSets
		manifestWorkReplicaSets, err := clients.WorkClient.WorkV1alpha1().ManifestWorkReplicaSets("").List(ctx, metav1.ListOptions{})
		if err == nil && len(manifestWorkReplicaSets.Items) > 0 {
			fmt.Fprintf(file, "\n=== ManifestWorkReplicaSets ===\n")
			for _, mwrs := range manifestWorkReplicaSets.Items {
				fmt.Fprintf(file, "- %s/%s\n", mwrs.Namespace, mwrs.Name)
			}
		}
	} else {
		// Collect spoke-specific CRs

		// Klusterlet
		klusterlets, err := clients.OperatorClient.OperatorV1().Klusterlets().List(ctx, metav1.ListOptions{})
		if err == nil && len(klusterlets.Items) > 0 {
			fmt.Fprintf(file, "\n=== Klusterlets ===\n")
			for _, kl := range klusterlets.Items {
				fmt.Fprintf(file, "- %s (DeployOption: %s, ClusterName: %s)\n",
					kl.Name,
					kl.Spec.DeployOption.Mode,
					kl.Spec.ClusterName)
			}
		}

		// AppliedManifestWorks
		appliedManifestWorks, err := clients.WorkClient.WorkV1().AppliedManifestWorks().List(ctx, metav1.ListOptions{})
		if err == nil && len(appliedManifestWorks.Items) > 0 {
			fmt.Fprintf(file, "\n=== AppliedManifestWorks ===\n")
			for _, amw := range appliedManifestWorks.Items {
				fmt.Fprintf(file, "- %s\n", amw.Name)
			}
		}

		// ClusterClaims
		clusterClaims, err := clients.ClusterClient.ClusterV1alpha1().ClusterClaims().List(ctx, metav1.ListOptions{})
		if err == nil && len(clusterClaims.Items) > 0 {
			fmt.Fprintf(file, "\n=== ClusterClaims ===\n")
			for _, cc := range clusterClaims.Items {
				fmt.Fprintf(file, "- %s: %s\n", cc.Name, cc.Spec.Value)
			}
		}
	}

	return nil
}
