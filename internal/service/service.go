// SPDX-License-Identifier: MIT

// Package service defines what the user interface can ask of the
// application: the Service interface, its data types and its errors. The
// Wails binding (internal/gui) wraps it; the engine (internal/engine)
// implements it. Nothing here imports Wails.
//
// M0 holds the part the shell needs; the trace, query and export methods
// (docs/ARCHITECTURE.md section 2) follow with M1 to M3, each with its
// TypeScript mirror and JSON Schema.
package service

import (
	"context"
	"fmt"
)

// Service is the application as the user interface sees it.
type Service interface {
	// About returns the versions of the application and its libraries.
	About(ctx context.Context) (About, error)
}

// About names the versions in this build.
type About struct {
	Version       string `json:"version"`
	GoModbusTrace string `json:"go_modbus_trace"`
	Wails         string `json:"wails"`
	Go            string `json:"go"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
}

// Error codes the frontend can act on.
const (
	CodeInvalidArgument = "invalid_argument"
	CodeNotFound        = "not_found"
	CodeCanceled        = "canceled"
	CodeInternal        = "internal"
)

// Error is the error of a Service method, as the frontend receives it.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

// Error returns the message.
func (e *Error) Error() string { return e.Message }

// Unwrap returns the underlying error.
func (e *Error) Unwrap() error { return e.Err }

// Errorf creates an Error with a formatted message.
func Errorf(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap creates an Error around err, with a formatted message in front.
func Wrap(code string, err error, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...) + ": " + err.Error(), Err: err}
}
