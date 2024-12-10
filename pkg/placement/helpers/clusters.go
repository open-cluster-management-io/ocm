package helpers

import (
	"regexp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

type ClusterSelector struct {
	labelSelector      labels.Selector
	labelRegexSelector regexSelector
	claimSelector      labels.Selector
	claimRegexSelector regexSelector
}

type regexSelector map[string]string

func (r *regexSelector) Matches(ls labels.Labels) bool {
	for key, pattern := range *r {
		// Check if the key exists in inputLabels
		if !ls.Has(key) {
			return false
		}

		// Compile the regex pattern
		regex, err := regexp.Compile(pattern)
		if err != nil {
			// Handle invalid regex pattern
			klog.Warningf("incorrect regex expression: %v", err)
			return false
		}

		// Check if the value matches the regex
		if !regex.MatchString(ls.Get(key)) {
			return false
		}
	}
	return true
}

func NewClusterSelector(selector clusterapiv1beta1.ClusterSelector) (*ClusterSelector, error) {
	// build label selector
	labelSelector, err := convertLabelSelector(&selector.LabelSelector.LabelSelector)
	if err != nil {
		return nil, err
	}
	// build claim selector
	claimSelector, err := convertClaimSelector(&selector.ClaimSelector)
	if err != nil {
		return nil, err
	}
	return &ClusterSelector{
		labelSelector:      labelSelector,
		labelRegexSelector: selector.LabelSelector.RegexSelector,
		claimSelector:      claimSelector,
		claimRegexSelector: selector.ClaimSelector.RegexSelector,
	}, nil
}

func (c *ClusterSelector) Matches(clusterlabels, clusterclaims map[string]string) bool {
	// match with label selector
	if ok := c.labelSelector.Matches(labels.Set(clusterlabels)); !ok {
		return false
	}
	// match with label regex selector
	if ok := c.labelRegexSelector.Matches(labels.Set(clusterlabels)); !ok {
		return false
	}
	// match with claim selector
	if ok := c.claimSelector.Matches(labels.Set(clusterclaims)); !ok {
		return false
	}
	// match with claim regex selector
	if ok := c.claimRegexSelector.Matches(labels.Set(clusterclaims)); !ok {
		return false
	}
	return true
}

// convertLabelSelector converts metav1.LabelSelector to labels.Selector
func convertLabelSelector(labelSelector *metav1.LabelSelector) (labels.Selector, error) {
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return labels.Nothing(), err
	}

	return selector, nil
}

// convertClaimSelector converts ClusterClaimSelector to labels.Selector
func convertClaimSelector(clusterClaimSelector *clusterapiv1beta1.ClusterClaimSelector) (labels.Selector, error) {
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchExpressions: clusterClaimSelector.MatchExpressions,
	})
	if err != nil {
		return labels.Nothing(), err
	}

	return selector, nil
}

// GetClusterClaims returns a map containing cluster claims from the status of cluster
func GetClusterClaims(cluster *clusterapiv1.ManagedCluster) map[string]string {
	claims := map[string]string{}
	for _, claim := range cluster.Status.ClusterClaims {
		claims[claim.Name] = claim.Value
	}
	return claims
}
