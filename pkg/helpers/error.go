package helpers

import (
	"fmt"
	"time"
)

type RequeueError struct {
	RequeueTime time.Duration
	Message     string
}

func (r *RequeueError) Error() string {
	return fmt.Sprintf(r.Message+", Requeue time: %v", r.RequeueTime)
}

func NewRequeueError(msg string, requeueTime time.Duration) *RequeueError {
	return &RequeueError{
		RequeueTime: requeueTime,
		Message:     msg,
	}
}
