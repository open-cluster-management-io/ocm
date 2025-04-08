package errors

import (
	"fmt"
	"net/http"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const StatusReasonPublishError metav1.StatusReason = "PublishError"

// NewPublishError returns an error indicating a resource could not be published, and the client can try again.
func NewPublishError(qualifiedResource schema.GroupResource, name string, err error) *errors.StatusError {
	return &errors.StatusError{
		ErrStatus: metav1.Status{
			Status: metav1.StatusFailure,
			Code:   http.StatusInternalServerError,
			Reason: StatusReasonPublishError,
			Details: &metav1.StatusDetails{
				Group:  qualifiedResource.Group,
				Kind:   qualifiedResource.Resource,
				Name:   name,
				Causes: []metav1.StatusCause{{Message: err.Error()}},
			},
			Message: fmt.Sprintf("Failed to publish work %s: %v", name, err),
		},
	}
}

// IsPublishError determines if err is a publish error which indicates that the request can be retried
// by the client.
func IsPublishError(err error) bool {
	return errors.ReasonForError(err) == StatusReasonPublishError
}
