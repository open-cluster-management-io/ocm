package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	. "github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// diagnosticsDirEnv enables saving cluster state when a spec fails (in JustAfterEach) and before
// AfterSuite cleanup. Both run before cleanup removes the agents involved (e.g. the universal
// klusterlet), so their logs are still available.
const diagnosticsDirEnv = "E2E_DIAGNOSTICS_DIR"

var nonAlphanumeric = regexp.MustCompile(`[^a-zA-Z0-9]+`)

var _ = JustAfterEach(func() {
	if report := CurrentSpecReport(); report.Failed() {
		saveDiagnosticsIfEnabled(report.FullText())
	}
})

func saveDiagnosticsIfEnabled(label string) {
	root := os.Getenv(diagnosticsDirEnv)
	if root == "" {
		return
	}
	name := nonAlphanumeric.ReplaceAllString(label, "-")
	if len(name) > 100 {
		name = name[:100]
	}
	dir := filepath.Join(root, fmt.Sprintf("%s-%s", time.Now().UTC().Format("150405"), name))
	if err := saveDiagnostics(dir); err != nil {
		// Never fail the suite because diagnostics could not be saved.
		GinkgoWriter.Printf("failed to save diagnostics to %s: %v\n", dir, err)
		return
	}
	GinkgoWriter.Printf("saved diagnostics to %s\n", dir)
}

func saveDiagnostics(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o750); err != nil {
		return err
	}

	csrs, err := hub.KubeClient.CertificatesV1().CertificateSigningRequests().List(ctx, metav1.ListOptions{})
	if err == nil {
		writeYAML(filepath.Join(dir, "csrs.yaml"), csrs)
	}
	events, err := hub.KubeClient.CoreV1().Events(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err == nil {
		writeYAML(filepath.Join(dir, "events.yaml"), events)
	}
	if clusters, err := hub.ClusterClient.ClusterV1().ManagedClusters().List(ctx, metav1.ListOptions{}); err == nil {
		writeYAML(filepath.Join(dir, "managedclusters.yaml"), clusters)
	}
	if addons, err := hub.AddonClient.AddonV1alpha1().ManagedClusterAddOns(metav1.NamespaceAll).List(
		ctx, metav1.ListOptions{}); err == nil {
		writeYAML(filepath.Join(dir, "managedclusteraddons.yaml"), addons)
	}

	pods, err := hub.KubeClient.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing pods: %w", err)
	}
	writeYAML(filepath.Join(dir, "pods.yaml"), pods)
	tail := int64(5000)
	for _, pod := range pods.Items {
		if pod.Namespace == "kube-system" || pod.Namespace == "local-path-storage" {
			continue
		}
		for _, c := range pod.Spec.Containers {
			saveLog(ctx, dir, pod, c.Name, false, tail)
		}
		// Logs of a crashed container explain restarts that the current logs don't show.
		for _, s := range pod.Status.ContainerStatuses {
			if s.RestartCount > 0 {
				saveLog(ctx, dir, pod, s.Name, true, tail)
			}
		}
	}
	return nil
}

func saveLog(ctx context.Context, dir string, pod corev1.Pod, container string, previous bool, tail int64) {
	data, err := hub.KubeClient.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container: container, Previous: previous, TailLines: &tail,
	}).DoRaw(ctx)
	if err != nil {
		data = []byte(fmt.Sprintf("failed to get logs: %v\n", err))
	}
	suffix := ""
	if previous {
		suffix = ".previous"
	}
	file := filepath.Join(dir, "logs", fmt.Sprintf("%s_%s_%s%s.log", pod.Namespace, pod.Name, container, suffix))
	_ = os.WriteFile(file, data, 0o600)
}

func writeYAML(file string, obj interface{}) {
	data, err := yaml.Marshal(obj)
	if err != nil {
		data = []byte(fmt.Sprintf("failed to marshal: %v\n", err))
	}
	_ = os.WriteFile(file, data, 0o600)
}
