package helpers

import (
	"reflect"
	"testing"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
)

func TestFilterClusterLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   map[string]string
	}{
		{
			name:   "nil labels",
			labels: nil,
			want:   map[string]string{},
		},
		{
			name:   "empty labels",
			labels: map[string]string{},
			want:   map[string]string{},
		},
		{
			name: "plain user labels are kept",
			labels: map[string]string{
				"env":    "prod",
				"region": "us-west",
			},
			want: map[string]string{
				"env":    "prod",
				"region": "us-west",
			},
		},
		{
			name: "clusterset label is dropped",
			labels: map[string]string{
				clusterv1beta2.ClusterSetLabel: "team-a",
				"env":                          "prod",
			},
			want: map[string]string{
				"env": "prod",
			},
		},
		{
			name: "cluster-name label is dropped",
			labels: map[string]string{
				clusterv1.ClusterNameLabelKey: "cluster1",
				"team":                        "platform",
			},
			want: map[string]string{
				"team": "platform",
			},
		},
		{
			name: "any open-cluster-management.io subdomain is dropped",
			labels: map[string]string{
				"feature.open-cluster-management.io/addon": "enabled",
				"env": "dev",
			},
			want: map[string]string{
				"env": "dev",
			},
		},
		{
			name: "trailing slash on the reserved domain is dropped",
			labels: map[string]string{
				"open-cluster-management.io/": "x",
			},
			want: map[string]string{},
		},
		{
			name: "reserved domain used as a bare name is kept",
			labels: map[string]string{
				"open-cluster-management.io": "x",
				"env":                        "prod",
			},
			want: map[string]string{
				"open-cluster-management.io": "x",
				"env":                        "prod",
			},
		},
		{
			name: "other vendor domains are kept",
			labels: map[string]string{
				"example.com/team": "blue",
			},
			want: map[string]string{
				"example.com/team": "blue",
			},
		},
		{
			name: "lookalike domain is not treated as reserved",
			labels: map[string]string{
				"myopen-cluster-management.io/foo": "bar",
			},
			want: map[string]string{
				"myopen-cluster-management.io/foo": "bar",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FilterClusterLabels(tt.labels); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FilterClusterLabels() = %v, want %v", got, tt.want)
			}
		})
	}
}
