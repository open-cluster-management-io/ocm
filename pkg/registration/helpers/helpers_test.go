package helpers

import (
	"reflect"
	"testing"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func TestIsValidHTTPSURL(t *testing.T) {
	cases := []struct {
		name      string
		serverURL string
		isValid   bool
	}{
		{
			name:      "an empty url",
			serverURL: "",
			isValid:   false,
		},
		{
			name:      "an invalid url",
			serverURL: "/path/path/path",
			isValid:   false,
		},
		{
			name:      "a http url",
			serverURL: "http://127.0.0.1:8080",
			isValid:   false,
		},
		{
			name:      "a https url",
			serverURL: "https://127.0.0.1:6443",
			isValid:   true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isValid := IsValidHTTPSURL(c.serverURL)
			if isValid != c.isValid {
				t.Errorf("expected %t, but %t", c.isValid, isValid)
			}
		})
	}
}

func TestFindTaintByKey(t *testing.T) {
	cases := []struct {
		name     string
		cluster  *clusterv1.ManagedCluster
		key      string
		expected *clusterv1.Taint
	}{
		{
			name: "nil of managed cluster",
			key:  "taint1",
		},
		{
			name: "taint found",
			cluster: &clusterv1.ManagedCluster{
				Spec: clusterv1.ManagedClusterSpec{
					Taints: []clusterv1.Taint{
						{
							Key:   "taint1",
							Value: "value1",
						},
					},
				},
			},
			key: "taint1",
			expected: &clusterv1.Taint{
				Key:   "taint1",
				Value: "value1",
			},
		},
		{
			name: "taint not found",
			cluster: &clusterv1.ManagedCluster{
				Spec: clusterv1.ManagedClusterSpec{
					Taints: []clusterv1.Taint{
						{
							Key:   "taint1",
							Value: "value1",
						},
					},
				},
			},
			key: "taint2",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := FindTaintByKey(c.cluster, c.key)
			if !reflect.DeepEqual(actual, c.expected) {
				t.Errorf("expected %v but got %v", c.expected, actual)
			}
		})
	}
}

var (
	UnavailableTaint = clusterv1.Taint{
		Key:    clusterv1.ManagedClusterTaintUnavailable,
		Effect: clusterv1.TaintEffectNoSelect,
	}

	UnreachableTaint = clusterv1.Taint{
		Key:    clusterv1.ManagedClusterTaintUnreachable,
		Effect: clusterv1.TaintEffectNoSelect,
	}
)

func TestIsTaintsEqual(t *testing.T) {
	cases := []struct {
		name    string
		taints1 []clusterv1.Taint
		taints2 []clusterv1.Taint
		expect  bool
	}{
		{
			name:    "two empty taints",
			taints1: []clusterv1.Taint{},
			taints2: []clusterv1.Taint{},
			expect:  true,
		},
		{
			name:    "two nil taints",
			taints1: nil,
			taints2: nil,
			expect:  true,
		},
		{
			name:    "len(taints1) = 1, len(taints2) = 0",
			taints1: []clusterv1.Taint{UnavailableTaint},
			taints2: []clusterv1.Taint{},
			expect:  false,
		},
		{
			name:    "taints1 is the same as taints",
			taints1: []clusterv1.Taint{UnreachableTaint},
			taints2: []clusterv1.Taint{UnreachableTaint},
			expect:  true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := reflect.DeepEqual(c.taints1, c.taints2)
			if actual != c.expect {
				t.Errorf("expected %t, but %t", c.expect, actual)
			}
		})
	}
}

func TestAddTaints(t *testing.T) {
	cases := []struct {
		name          string
		taints        []clusterv1.Taint
		addTaint      clusterv1.Taint
		resTaints     []clusterv1.Taint
		expectUpdated bool
	}{
		{
			name:          "add taint success",
			taints:        []clusterv1.Taint{},
			addTaint:      UnreachableTaint,
			expectUpdated: true,
			resTaints:     []clusterv1.Taint{UnreachableTaint},
		},
		{
			name:          "add taint fail, taint already exists",
			taints:        []clusterv1.Taint{UnreachableTaint},
			addTaint:      UnreachableTaint,
			expectUpdated: false,
			resTaints:     []clusterv1.Taint{UnreachableTaint},
		},
		{
			name:          "nil pointer judgment",
			taints:        nil,
			addTaint:      UnreachableTaint,
			expectUpdated: true,
			resTaints:     []clusterv1.Taint{UnreachableTaint},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			taints := c.taints
			updated := AddTaints(&taints, c.addTaint)
			c.taints = taints
			if updated != c.expectUpdated {
				t.Errorf("updated expected %t, but %t", c.expectUpdated, updated)
			}
			if !reflect.DeepEqual(c.taints, c.resTaints) {
				t.Errorf("taints expected %+v, but %+v", c.taints, c.resTaints)
			}
		})
	}
}

func TestRemoveTaints(t *testing.T) {
	cases := []struct {
		name          string
		taints        []clusterv1.Taint
		removeTaint   clusterv1.Taint
		resTaints     []clusterv1.Taint
		expectUpdated bool
	}{
		{
			name:          "nil pointer judgment",
			taints:        nil,
			removeTaint:   UnreachableTaint,
			expectUpdated: false,
			resTaints:     nil,
		},
		{
			name:          "remove success",
			taints:        []clusterv1.Taint{UnreachableTaint},
			removeTaint:   UnreachableTaint,
			expectUpdated: true,
			resTaints:     []clusterv1.Taint{},
		},
		{
			name:          "remove taint failed, taint not exists",
			taints:        []clusterv1.Taint{UnreachableTaint},
			removeTaint:   UnavailableTaint,
			expectUpdated: false,
			resTaints:     []clusterv1.Taint{UnreachableTaint},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			taints := c.taints
			updated := RemoveTaints(&taints, c.removeTaint)
			c.taints = taints
			if updated != c.expectUpdated {
				t.Errorf("updated expected %t, but %t", c.expectUpdated, updated)
			}
			if !reflect.DeepEqual(c.taints, c.resTaints) {
				t.Errorf("taints expected %+v, but %+v", c.taints, c.resTaints)
			}
		})
	}
}
