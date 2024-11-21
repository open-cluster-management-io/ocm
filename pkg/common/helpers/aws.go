package helpers

import (
	"regexp"
)

// IsEksArnWellFormed checks if the EKS cluster ARN is well-formed
// Example of a well-formed ARN: arn:aws:eks:us-west-2:123456789012:cluster/my-cluster
func IsEksArnWellFormed(eksArn string) bool {
	pattern := "^arn:aws:eks:([a-zA-Z0-9-]+):(\\d{12}):cluster/([a-zA-Z0-9-]+)$"
	matched, err := regexp.MatchString(pattern, eksArn)
	if err != nil {
		return false
	}
	return matched
}
