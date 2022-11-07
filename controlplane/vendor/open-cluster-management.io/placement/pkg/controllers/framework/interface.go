package framework

import (
	"errors"
	"strings"
)

// Code is the Status code/type which is returned from the framework.
type Code int

// These are predefined codes used in a Status.
const (
	// Success means that plugin ran correctly and found placement schedulable.
	// NOTE: A nil status is also considered as "Success".
	Success Code = iota
	// Warning means that plugin ran correctly and found placement schedulable, but with some failures to notice.
	Warning
	// Error is used for internal plugin errors etc.
	Error
	// Misconfigured is used for internal plugin configuration errors, unexpected input, etc.
	Misconfigured
)

type Status struct {
	code Code
	// reasons contains the message about status.
	reasons []string
	// Err contains the error message.
	err error
	// plugin is an optional field that records the plugin name.
	plugin string
}

// Code returns code of the Status.
func (s *Status) Code() Code {
	if s == nil {
		return Success
	}
	return s.code
}

// Message returns a concatenated message on reasons of the Status.
func (s *Status) Message() string {
	if s == nil {
		return ""
	}
	return strings.Join(s.reasons, ", ")
}

// AppendReason appends given reason to the Status.
func (s *Status) AppendReason(reason string) {
	s.reasons = append(s.reasons, reason)
}

// IsSuccess returns true if and only if "Status" is nil or Code is "Success".
func (s *Status) IsSuccess() bool {
	return s.Code() == Success
}

// IsError returns true if and only if Code is "Error" or "Misconfigured".
func (s *Status) IsError() bool {
	switch s.Code() {
	case Error:
		return true
	case Misconfigured:
		return true
	default:
		return false
	}
}

// AsError returns nil if the status is a success; otherwise returns an "error" object
// with a concatenated message on reasons of the Status.
func (s *Status) AsError() error {
	if s.Code() == Success {
		return nil
	}
	if s.err != nil {
		return s.err
	}
	return errors.New(s.Message())
}

// FailedPlugin returns the failed plugin name.
func (s *Status) Plugin() string {
	return s.plugin
}

// NewStatus makes a Status out of the given arguments and returns its pointer.
func NewStatus(plugin string, code Code, reasons ...string) *Status {
	s := &Status{
		code:    code,
		reasons: reasons,
		plugin:  plugin,
	}
	if code == Error {
		s.err = errors.New(s.Message())
	}
	return s
}
