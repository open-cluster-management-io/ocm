package queue

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

func TestFileter(t *testing.T) {
	tc := []struct {
		name     string
		filter   factory.EventFilterFunc
		object   runtime.Object
		filtered bool
	}{
		{
			name:     "filter by label with no label",
			filter:   FileterByLabel("test"),
			object:   &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test", Labels: map[string]string{"test1": "value1"}}},
			filtered: false,
		},
		{
			name:     "filter by label with label",
			filter:   FileterByLabel("test"),
			object:   &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test", Labels: map[string]string{"test": "value1"}}},
			filtered: true,
		},
		{
			name:     "filter by label with incorrect labelkeyvalue",
			filter:   FileterByLabelKeyValue("test", "value"),
			object:   &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test", Labels: map[string]string{"test": "value1"}}},
			filtered: false,
		},
		{
			name:     "filter by label with incorrect labelkeyvalue",
			filter:   FileterByLabelKeyValue("test", "value1"),
			object:   &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test", Labels: map[string]string{"test": "value1"}}},
			filtered: true,
		},
		{
			name:     "filter by unmatched name",
			filter:   FilterByNames("test"),
			object:   &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test1"}},
			filtered: false,
		},
		{
			name:     "filter by matched name",
			filter:   FilterByNames("test", "test1"),
			object:   &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test1"}},
			filtered: true,
		},
		{
			name:     "uniion filter by unmatched",
			filter:   UnionFilter(FilterByNames("test"), FileterByLabel("test")),
			object:   &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
			filtered: false,
		},
		{
			name:     "uniion filter by matched",
			filter:   UnionFilter(FilterByNames("test"), FileterByLabel("test")),
			object:   &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test", Labels: map[string]string{"test": "value"}}},
			filtered: true,
		},
	}

	for _, c := range tc {
		t.Run(c.name, func(t *testing.T) {
			actual := c.filter(c.object)
			if c.filtered != actual {
				t.Errorf("expect filter %v, but got %v", c.filtered, actual)
			}
		})
	}
}

func TestQueueKey(t *testing.T) {
	tc := []struct {
		name         string
		queueKeyFunc factory.ObjectQueueKeysFunc
		object       runtime.Object
		expecteKey   []string
	}{
		{
			name:         "by name",
			queueKeyFunc: QueueKeyByMetaName,
			object:       &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "testns"}},
			expecteKey:   []string{"test"},
		},
		{
			name:         "by namespace",
			queueKeyFunc: QueueKeyByMetaNamespace,
			object:       &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "testns"}},
			expecteKey:   []string{"testns"},
		},
		{
			name:         "by namespace name",
			queueKeyFunc: QueueKeyByMetaNamespaceName,
			object:       &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "testns"}},
			expecteKey:   []string{"testns/test"},
		},
		{
			name:         "by emptey label",
			queueKeyFunc: QueueKeyByLabel("testlabel"),
			object:       &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "testns"}},
			expecteKey:   []string{},
		},
		{
			name:         "by matched label",
			queueKeyFunc: QueueKeyByLabel("testlabel"),
			object: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name: "test", Namespace: "testns", Labels: map[string]string{"testlabel": "value"}}},
			expecteKey: []string{"value"},
		},
	}

	for _, c := range tc {
		t.Run(c.name, func(t *testing.T) {
			actual := c.queueKeyFunc(c.object)
			if !reflect.DeepEqual(c.expecteKey, actual) {
				t.Errorf("expect key %v, but got %v", c.expecteKey, actual)
			}
		})
	}
}
