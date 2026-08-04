// Package exit defines the CLI's stable exit-code contract and the classifier
// that maps an error onto one of the codes. Agents and scripts branch on the
// exit code instead of parsing stderr, so this table is an API: keep it
// stable. Codes carries the name and meaning of every code, and the generated
// manual renders it, so the contract is described in one place.
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

// Documented pairs a code with the short name and the meaning the CLI
// publishes for it.
type Documented struct {
	Code        Code
	Name        string
	Description string
}

// Codes is the published exit-code contract, in code order. The generated
// manual renders this directly, so a code's meaning is written once.
var Codes = []Documented{
	{OK, "ok", "Success."},
	{Err, "error", "The command failed for a reason that does not fit a more specific category."},
	{Usage, "usage", "Invalid command syntax, argument count, or value; or the API rejected the request as invalid (HTTP 400 or 422)."},
	{Auth, "auth", "Missing or invalid credentials, unauthorized access, or permission denied (HTTP 401 or 403)."},
	{NotFound, "not-found", "A requested resource, phone allocation, or on-screen element was not found (HTTP 404)."},
	{Timeout, "timeout", "The operation exceeded its timeout or deadline (HTTP 408)."},
	{Unavailable, "unavailable", "The Axilio service or phone connection was unavailable, the phone was offline, the request was rate-limited, or the server failed (HTTP 429 or 5xx)."},
	{Canceled, "canceled", "The operation was canceled by the user, shell, or system."},
}

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
