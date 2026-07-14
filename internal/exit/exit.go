// Package exit defines the CLI's stable exit-code contract and the classifier
// that maps an error onto one of the codes. Agents and scripts branch on the
// exit code instead of parsing stderr, so this table is an API: keep it stable,
// and document it wherever the CLI is documented (README).
//
//	0  ok           success
//	1  error        generic / unclassified failure
//	2  usage        bad flags, args, or input (invalid_args, HTTP 400/422)
//	3  auth         missing key or unauthorized (unauthorized, HTTP 401/403)
//	4  not-found    element/session/resource not found (element_not_found,
//	                no_allocation, HTTP 404)
//	5  timeout      deadline exceeded, retryable (timeout, HTTP 408)
//	6  unavailable  network/device/server, transient (connection, not_connected,
//	                device_offline, HTTP 429/5xx)
//	7  canceled     the operation was canceled
//
// Classification precedence: an explicit code stamped with With wins, then the
// driver's mobile.Error taxonomy, then the SDK's HTTP status, then context
// deadline/cancel, else a generic error.
package exit

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/axilioai/platform-go/core"
	"github.com/axilioai/platform-go/drivers/mobile"
)

// Code is a stable, documented CLI exit status.
type Code int

const (
	OK          Code = 0
	Err         Code = 1
	Usage       Code = 2
	Auth        Code = 3
	NotFound    Code = 4
	Timeout     Code = 5
	Unavailable Code = 6
	Canceled    Code = 7
)

// coded carries an explicit exit code alongside an error, so a command can
// classify its own failure (e.g. a bad flag as Usage) without a sentinel.
type coded struct {
	code Code
	err  error
}

func (e *coded) Error() string { return e.err.Error() }
func (e *coded) Unwrap() error { return e.err }

// With stamps err with an explicit exit code that Classify returns verbatim.
func With(code Code, err error) error {
	if err == nil {
		return nil
	}
	return &coded{code: code, err: err}
}

// Usagef returns a Usage-coded (exit 2) error, for bad flags/args/input.
func Usagef(format string, a ...any) error {
	return With(Usage, fmt.Errorf(format, a...))
}

// Authf returns an Auth-coded (exit 3) error, for missing/rejected credentials.
func Authf(format string, a ...any) error {
	return With(Auth, fmt.Errorf(format, a...))
}

// Classify maps err onto its exit code. nil is OK.
func Classify(err error) Code {
	if err == nil {
		return OK
	}
	// 1. explicit override.
	var ce *coded
	if errors.As(err, &ce) {
		return ce.code
	}
	// 2. the driver's error taxonomy.
	var me *mobile.Error
	if errors.As(err, &me) {
		return fromMobile(me.Code)
	}
	// 3. the SDK's HTTP status.
	var ae *core.APIError
	if errors.As(err, &ae) {
		return fromStatus(ae.StatusCode)
	}
	// 4. context deadline / cancel.
	if errors.Is(err, context.DeadlineExceeded) {
		return Timeout
	}
	if errors.Is(err, context.Canceled) {
		return Canceled
	}
	// 5. cobra's arg/flag parse errors are untyped strings; a malformed
	// invocation is a usage error (exit 2), matching the bad-flag path. This
	// mirrors fang's own isUsageError so what fang renders as usage also exits 2.
	if isUsageText(err.Error()) {
		return Usage
	}
	return Err
}

// isUsageText reports whether s is one of cobra's stable arg/flag parse
// messages. Cobra returns these as plain errors, so a prefix match is the only
// way to classify them (the same approach fang uses to render them).
func isUsageText(s string) bool {
	for _, prefix := range []string{
		"flag needs an argument:",
		"unknown flag:",
		"unknown shorthand flag:",
		"unknown command",
		"invalid argument",
		"accepts ",  // ExactArgs/RangeArgs: "accepts N arg(s), received M"
		"requires ", // MinimumNArgs: "requires at least N arg(s)"
	} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func fromMobile(c mobile.Code) Code {
	switch c {
	case mobile.CodeUnauthorized:
		return Auth
	case mobile.CodeInvalidArgs:
		return Usage
	case mobile.CodeElementNotFound, mobile.CodeNoAllocation:
		return NotFound
	case mobile.CodeTimeout:
		return Timeout
	case mobile.CodeConnection, mobile.CodeNotConnected, mobile.CodeDeviceOffline:
		return Unavailable
	case mobile.CodeCanceled:
		return Canceled
	default: // CodeInternal, CodeUnknownOp, and anything new.
		return Err
	}
}

func fromStatus(status int) Code {
	switch status {
	case 400, 422:
		return Usage
	case 401, 403:
		return Auth
	case 404:
		return NotFound
	case 408:
		return Timeout
	case 429:
		return Unavailable
	default:
		if status >= 500 {
			return Unavailable
		}
		return Err
	}
}
