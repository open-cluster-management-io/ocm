package helpers

import (
	"context"
	"testing"
	"time"

	clusterfake "github.com/open-cluster-management/api/client/cluster/clientset/versioned/fake"
	clusterv1 "github.com/open-cluster-management/api/cluster/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/diff"
)

func TestUpdateStatusCondition(t *testing.T) {
	nowish := metav1.Now()
	beforeish := metav1.Time{Time: nowish.Add(-10 * time.Second)}
	afterish := metav1.Time{Time: nowish.Add(10 * time.Second)}

	cases := []struct {
		name               string
		startingConditions []clusterv1.StatusCondition
		newCondition       clusterv1.StatusCondition
		expextedUpdated    bool
		expectedConditions []clusterv1.StatusCondition
	}{
		{
			name:               "add to empty",
			startingConditions: []clusterv1.StatusCondition{},
			newCondition:       newCondition("test", "True", "my-reason", "my-message", nil),
			expextedUpdated:    true,
			expectedConditions: []clusterv1.StatusCondition{newCondition("test", "True", "my-reason", "my-message", nil)},
		},
		{
			name: "add to non-conflicting",
			startingConditions: []clusterv1.StatusCondition{
				newCondition("two", "True", "my-reason", "my-message", nil),
			},
			newCondition:    newCondition("one", "True", "my-reason", "my-message", nil),
			expextedUpdated: true,
			expectedConditions: []clusterv1.StatusCondition{
				newCondition("two", "True", "my-reason", "my-message", nil),
				newCondition("one", "True", "my-reason", "my-message", nil),
			},
		},
		{
			name: "change existing status",
			startingConditions: []clusterv1.StatusCondition{
				newCondition("two", "True", "my-reason", "my-message", nil),
				newCondition("one", "True", "my-reason", "my-message", nil),
			},
			newCondition:    newCondition("one", "False", "my-different-reason", "my-othermessage", nil),
			expextedUpdated: true,
			expectedConditions: []clusterv1.StatusCondition{
				newCondition("two", "True", "my-reason", "my-message", nil),
				newCondition("one", "False", "my-different-reason", "my-othermessage", nil),
			},
		},
		{
			name: "leave existing transition time",
			startingConditions: []clusterv1.StatusCondition{
				newCondition("two", "True", "my-reason", "my-message", nil),
				newCondition("one", "True", "my-reason", "my-message", &beforeish),
			},
			newCondition:    newCondition("one", "True", "my-reason", "my-message", &afterish),
			expextedUpdated: false,
			expectedConditions: []clusterv1.StatusCondition{
				newCondition("two", "True", "my-reason", "my-message", nil),
				newCondition("one", "True", "my-reason", "my-message", &beforeish),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeClusterClient := clusterfake.NewSimpleClientset(&clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "testspokecluster"},
				Status: clusterv1.ManagedClusterStatus{
					Conditions: c.startingConditions,
				},
			})

			status, updated, err := UpdateManagedClusterStatus(
				context.TODO(),
				fakeClusterClient,
				"testspokecluster",
				UpdateManagedClusterConditionFn(c.newCondition),
			)
			if err != nil {
				t.Errorf("unexpected err: %v", err)
			}
			if updated != c.expextedUpdated {
				t.Errorf("expected %t, but %t", c.expextedUpdated, updated)
			}
			for i := range c.expectedConditions {
				expected := c.expectedConditions[i]
				actual := status.Conditions[i]
				if expected.LastTransitionTime == (metav1.Time{}) {
					actual.LastTransitionTime = metav1.Time{}
				}
				if !equality.Semantic.DeepEqual(expected, actual) {
					t.Errorf(diff.ObjectDiff(expected, actual))
				}
			}
		})
	}
}

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

func newCondition(name, status, reason, message string, lastTransition *metav1.Time) clusterv1.StatusCondition {
	ret := clusterv1.StatusCondition{
		Type:    name,
		Status:  metav1.ConditionStatus(status),
		Reason:  reason,
		Message: message,
	}
	if lastTransition != nil {
		ret.LastTransitionTime = *lastTransition
	}
	return ret
}
