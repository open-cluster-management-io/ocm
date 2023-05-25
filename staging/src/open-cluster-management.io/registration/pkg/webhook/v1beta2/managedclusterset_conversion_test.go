package v1beta2

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/api/cluster/v1beta2"

	internalv1beta1 "open-cluster-management.io/registration/pkg/webhook/v1beta1"
)

func TestConvertTo(t *testing.T) {
	cases := []struct {
		name           string
		srcSet         *ManagedClusterSet
		expectedDstSet *internalv1beta1.ManagedClusterSet
	}{
		{
			name: "test nil spec set",
			srcSet: &ManagedClusterSet{
				v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcs1",
					},
				},
			},
			expectedDstSet: &internalv1beta1.ManagedClusterSet{
				ManagedClusterSet: v1beta1.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcs1",
					},
					Spec: v1beta1.ManagedClusterSetSpec{
						ClusterSelector: v1beta1.ManagedClusterSelector{
							SelectorType: v1beta1.LegacyClusterSetLabel,
						},
					},
				},
			},
		},
		{
			name: "test empty spec set",
			srcSet: &ManagedClusterSet{
				v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcs1",
					},
					Spec: v1beta2.ManagedClusterSetSpec{},
				},
			},
			expectedDstSet: &internalv1beta1.ManagedClusterSet{
				ManagedClusterSet: v1beta1.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcs1",
					},
					Spec: v1beta1.ManagedClusterSetSpec{
						ClusterSelector: v1beta1.ManagedClusterSelector{
							SelectorType: v1beta1.LegacyClusterSetLabel,
						},
					},
				},
			},
		},
		{
			name: "test exclusive set",
			srcSet: &ManagedClusterSet{
				v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcs1",
					},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.ExclusiveClusterSetLabel,
						},
					},
				},
			},
			expectedDstSet: &internalv1beta1.ManagedClusterSet{
				ManagedClusterSet: v1beta1.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcs1",
					},
					Spec: v1beta1.ManagedClusterSetSpec{
						ClusterSelector: v1beta1.ManagedClusterSelector{
							SelectorType: v1beta1.LegacyClusterSetLabel,
						},
					},
				},
			},
		},
		{
			name: "test labelselector set",
			srcSet: &ManagedClusterSet{
				v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcs1",
					},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.LabelSelector,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"k": "v",
								},
							},
						},
					},
				},
			},
			expectedDstSet: &internalv1beta1.ManagedClusterSet{
				ManagedClusterSet: v1beta1.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcs1",
					},
					Spec: v1beta1.ManagedClusterSetSpec{
						ClusterSelector: v1beta1.ManagedClusterSelector{
							SelectorType: v1beta1.LabelSelector,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"k": "v",
								},
							}},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dstSet := &internalv1beta1.ManagedClusterSet{}
			err := c.srcSet.ConvertTo(dstSet)
			if err != nil {
				t.Errorf("Failed to convert clusterset. %v", err)
			}
			if !reflect.DeepEqual(dstSet, c.expectedDstSet) {
				t.Errorf("Faild to convert clusterset. expectDstSet:%v , dstSet:%v", c.expectedDstSet, dstSet)
			}
		})
	}
}

func TestConvertFrom(t *testing.T) {
	cases := []struct {
		name           string
		srcSet         *internalv1beta1.ManagedClusterSet
		expectedDstSet *ManagedClusterSet
	}{
		{
			name: "test nil spec set",
			srcSet: &internalv1beta1.ManagedClusterSet{
				ManagedClusterSet: v1beta1.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcs1",
					},
				},
			},
			expectedDstSet: &ManagedClusterSet{
				ManagedClusterSet: v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcs1",
					},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.ExclusiveClusterSetLabel,
						},
					},
				},
			},
		},
		{
			name: "test empty spec set",
			srcSet: &internalv1beta1.ManagedClusterSet{
				ManagedClusterSet: v1beta1.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcs1",
					},
					Spec: v1beta1.ManagedClusterSetSpec{},
				},
			},
			expectedDstSet: &ManagedClusterSet{
				ManagedClusterSet: v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcs1",
					},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.ExclusiveClusterSetLabel,
						},
					},
				},
			},
		},
		{
			name: "test exclusive set",
			srcSet: &internalv1beta1.ManagedClusterSet{
				ManagedClusterSet: v1beta1.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcs1",
					},
					Spec: v1beta1.ManagedClusterSetSpec{
						ClusterSelector: v1beta1.ManagedClusterSelector{
							SelectorType: v1beta1.LegacyClusterSetLabel,
						},
					},
				},
			},
			expectedDstSet: &ManagedClusterSet{
				ManagedClusterSet: v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcs1",
					},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.ExclusiveClusterSetLabel,
						},
					},
				},
			},
		},
		{
			name: "test labelselector set",
			srcSet: &internalv1beta1.ManagedClusterSet{
				ManagedClusterSet: v1beta1.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcs1",
					},
					Spec: v1beta1.ManagedClusterSetSpec{
						ClusterSelector: v1beta1.ManagedClusterSelector{
							SelectorType: v1beta1.LabelSelector,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"k": "v",
								},
							},
						},
					},
				},
			},
			expectedDstSet: &ManagedClusterSet{
				ManagedClusterSet: v1beta2.ManagedClusterSet{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mcs1",
					},
					Spec: v1beta2.ManagedClusterSetSpec{
						ClusterSelector: v1beta2.ManagedClusterSelector{
							SelectorType: v1beta2.LabelSelector,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"k": "v",
								},
							}},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dstSet := &ManagedClusterSet{}
			err := dstSet.ConvertFrom(c.srcSet)
			if err != nil {
				t.Errorf("Failed to convert clusterset. %v", err)
			}
			if !reflect.DeepEqual(dstSet, c.expectedDstSet) {
				t.Errorf("Faild to convert clusterset. expectDstSet:%v , dstSet:%v", c.expectedDstSet, dstSet)
			}
		})
	}
}
