//go:build lintconfig

// Copyright Contributors to the Open Cluster Management project

package lintconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestStaticcheckBaseline(t *testing.T) {
	projectDir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(projectDir, ".golangci.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	fixtureDir := t.TempDir()
	t.Setenv("GOLANGCI_LINT_CACHE", filepath.Join(fixtureDir, "lint-cache"))
	writeFixture(t, fixtureDir, ".golangci.yaml", string(config))
	writeFixture(t, fixtureDir, "go.mod", "module example.com/lintconfig\n\ngo 1.26.0\n")
	writeFixture(t, fixtureDir, "pkg/common/helpers/aws.go", `package helpers

func GetAwsAccountIdAndClusterName(clusterArn string) (string, string) {
	return clusterArn, clusterArn
}

func NewAwsAccountId() string {
	return ""
}

func Same(a int) bool {
	return a == a
}

type Inner struct {
	Value int
}

type Outer struct {
	Inner
}

func Read(o Outer) int {
	return o.Inner.Value
}
`)
	writeFixture(t, fixtureDir, "pkg/other/aws.go", `package other

func GetAwsAccountIdAndClusterName(clusterArn string) (string, string) {
	return clusterArn, clusterArn
}
`)

	cmd := exec.Command(golangciLint(t), "run", "--enable-only=staticcheck", "--output.json.path=stdout", "--show-stats=false", "./...")
	cmd.Dir = fixtureDir
	output, err := cmd.Output()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("expected lint findings, got error %v and output %s", err, output)
	}
	var result struct {
		Issues []struct {
			Text string
			Pos  struct{ Filename string }
		}
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode lint output: %v\n%s\n%s", err, output, exitErr.Stderr)
	}

	hasIssue := func(path, message string) bool {
		for _, issue := range result.Issues {
			if issue.Pos.Filename == path && strings.Contains(issue.Text, message) {
				return true
			}
		}
		return false
	}
	for _, issue := range []struct{ path, message string }{
		{"pkg/common/helpers/aws.go", "ST1003: func NewAwsAccountId"},
		{"pkg/common/helpers/aws.go", "SA4000:"},
		{"pkg/other/aws.go", "ST1003: func GetAwsAccountIdAndClusterName"},
	} {
		if !hasIssue(issue.path, issue.message) {
			t.Errorf("missing %s in %s; got %+v", issue.message, issue.path, result.Issues)
		}
	}
	if hasIssue("pkg/common/helpers/aws.go", "QF1008:") {
		t.Error("QF1008 should remain excluded")
	}
	legacySource, err := os.ReadFile(filepath.Join(projectDir, "pkg/common/helpers/aws.go"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if strings.Contains(string(legacySource), "func GetAwsAccountIdAndClusterName(") &&
		hasIssue("pkg/common/helpers/aws.go", "ST1003: func GetAwsAccountIdAndClusterName") {
		t.Error("the existing naming finding should be excluded until it is fixed")
	}
}

func golangciLint(t *testing.T) string {
	t.Helper()
	var candidates []string
	if path, err := exec.LookPath("golangci-lint"); err == nil {
		candidates = append(candidates, path)
	}
	output, err := exec.Command("go", "env", "GOPATH").Output()
	if err != nil {
		t.Fatalf("find golangci-lint: %v", err)
	}
	for _, dir := range filepath.SplitList(strings.TrimSpace(string(output))) {
		candidates = append(candidates, filepath.Join(dir, "bin", "golangci-lint"))
	}
	var bestPath string
	var bestVersion [3]int
	for _, path := range candidates {
		output, err := exec.Command(path, "version", "--json").Output()
		if err != nil {
			continue
		}
		var info struct{ Version string }
		if err := json.Unmarshal(output, &info); err != nil {
			continue
		}
		var version [3]int
		if _, err := fmt.Sscanf(strings.TrimPrefix(info.Version, "v"), "%d.%d.%d", &version[0], &version[1], &version[2]); err != nil || version[0] != 2 {
			continue
		}
		if bestPath == "" || slices.Compare(version[:], bestVersion[:]) > 0 {
			bestPath, bestVersion = path, version
		}
	}
	if bestPath != "" {
		return bestPath
	}
	t.Fatal("golangci-lint not found; run make lint to install it")
	return ""
}

func writeFixture(t *testing.T, root, path, contents string) {
	t.Helper()
	name := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
