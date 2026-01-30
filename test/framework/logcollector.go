package framework

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	if err := collectClusterLogs(hub.KubeClient, hubDir, config, "hub"); err != nil {
		fmt.Printf("Warning: failed to collect hub logs: %v\n", err)
	}

	// Collect spoke cluster logs
	spokeDir := filepath.Join(config.OutputDir, "spoke")
	if err := os.MkdirAll(spokeDir, 0755); err != nil {
		return fmt.Errorf("failed to create spoke directory: %w", err)
	}
	if err := collectClusterLogs(spoke.KubeClient, spokeDir, config, "spoke"); err != nil {
		fmt.Printf("Warning: failed to collect spoke logs: %v\n", err)
	}

	fmt.Printf("Log collection completed in %s\n", config.OutputDir)
	return nil
}

// collectClusterLogs collects logs from a single cluster
func collectClusterLogs(client kubernetes.Interface, outputDir string, config *LogCollectorConfig, clusterType string) error {
	ctx := context.Background()

	// Define namespaces to collect logs from
	namespaces := getRelevantNamespaces(clusterType)

	for _, ns := range namespaces {
		nsDir := filepath.Join(outputDir, ns)
		if err := os.MkdirAll(nsDir, 0755); err != nil {
			fmt.Printf("Warning: failed to create namespace directory %s: %v\n", nsDir, err)
			continue
		}

		// Collect pod logs and descriptions
		if config.IncludePodLogs {
			if err := collectPodLogs(ctx, client, ns, nsDir, config.TailLines); err != nil {
				fmt.Printf("Warning: failed to collect pod logs for namespace %s: %v\n", ns, err)
			}
		}

		// Collect pod descriptions
		if err := collectPodDescriptions(ctx, client, ns, nsDir); err != nil {
			fmt.Printf("Warning: failed to collect pod descriptions for namespace %s: %v\n", ns, err)
		}

		// Collect events
		if config.IncludeEvents {
			if err := collectEvents(ctx, client, ns, nsDir); err != nil {
				fmt.Printf("Warning: failed to collect events for namespace %s: %v\n", ns, err)
			}
		}
	}

	// Collect cluster-wide resources
	if err := collectClusterResources(ctx, client, outputDir); err != nil {
		fmt.Printf("Warning: failed to collect cluster resources: %v\n", err)
	}

	return nil
}

// getRelevantNamespaces returns the list of namespaces to collect logs from
func getRelevantNamespaces(clusterType string) []string {
	baseNamespaces := []string{
		"open-cluster-management",
		"open-cluster-management-hub",
		"open-cluster-management-agent",
		"open-cluster-management-agent-addon",
	}

	// Always include kube-system for both hub and spoke
	return append(baseNamespaces, "kube-system")
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
func collectClusterResources(ctx context.Context, client kubernetes.Interface, outputDir string) error {
	filename := filepath.Join(outputDir, "cluster-resources.txt")
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create cluster resources file: %w", err)
	}
	defer file.Close()

	// List all nodes
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
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
	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err == nil {
		fmt.Fprintf(file, "\n=== Namespaces ===\n")
		for _, ns := range namespaces.Items {
			fmt.Fprintf(file, "- %s (Status: %s)\n", ns.Name, ns.Status.Phase)
		}
	}

	// List deployments in OCM namespaces
	ocmNamespaces := []string{
		"open-cluster-management",
		"open-cluster-management-hub",
		"open-cluster-management-agent",
	}

	for _, ns := range ocmNamespaces {
		deployments, err := client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
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

	return nil
}

// CollectResourceYAML collects YAML representation of specific resources
func CollectResourceYAML(ctx context.Context, client kubernetes.Interface, outputDir, resourceType, namespace, name string) error {
	// This is a placeholder for collecting specific resources as YAML
	// Can be extended based on specific needs
	return nil
}

// GetAllDynamicNamespaces finds all namespaces that match OCM patterns
func GetAllDynamicNamespaces(ctx context.Context, client kubernetes.Interface) ([]string, error) {
	namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var ocmNamespaces []string
	ocmPrefixes := []string{
		"open-cluster-management",
		"klusterlet-",
	}

	for _, ns := range namespaces.Items {
		for _, prefix := range ocmPrefixes {
			if strings.HasPrefix(ns.Name, prefix) {
				ocmNamespaces = append(ocmNamespaces, ns.Name)
				break
			}
		}
	}

	return ocmNamespaces, nil
}
