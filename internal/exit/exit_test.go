package exit

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/axilioai/platform-go/core"
	"github.com/axilioai/platform-go/drivers/mobile"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Code
	}{
		{"nil", nil, OK},
		{"generic", errors.New("boom"), Err},
		{"explicit usage", Usagef("bad flag"), Usage},
		{"explicit auth", Authf("no key"), Auth},
		{"wrapped explicit", fmt.Errorf("context: %w", Usagef("x")), Usage},

		// driver taxonomy
		{"driver unauthorized", &mobile.Error{Code: mobile.CodeUnauthorized}, Auth},
		{"driver invalid args", &mobile.Error{Code: mobile.CodeInvalidArgs}, Usage},
		{"driver element not found", &mobile.Error{Code: mobile.CodeElementNotFound}, NotFound},
		{"driver no allocation", &mobile.Error{Code: mobile.CodeNoAllocation}, NotFound},
		{"driver timeout", &mobile.Error{Code: mobile.CodeTimeout}, Timeout},
		{"driver connection", &mobile.Error{Code: mobile.CodeConnection}, Unavailable},
		{"driver not connected", &mobile.Error{Code: mobile.CodeNotConnected}, Unavailable},
		{"driver device offline", &mobile.Error{Code: mobile.CodeDeviceOffline}, Unavailable},
		{"driver canceled", &mobile.Error{Code: mobile.CodeCanceled}, Canceled},
		{"driver internal", &mobile.Error{Code: mobile.CodeInternal}, Err},
		{"driver unknown op", &mobile.Error{Code: mobile.CodeUnknownOp}, Err},

		// HTTP status
		{"http 400", core.NewAPIError(400, nil, errors.New("bad")), Usage},
		{"http 401", core.NewAPIError(401, nil, errors.New("nope")), Auth},
		{"http 403", core.NewAPIError(403, nil, errors.New("nope")), Auth},
		{"http 404", core.NewAPIError(404, nil, errors.New("gone")), NotFound},
		{"http 408", core.NewAPIError(408, nil, errors.New("slow")), Timeout},
		{"http 429", core.NewAPIError(429, nil, errors.New("busy")), Unavailable},
		{"http 500", core.NewAPIError(500, nil, errors.New("oops")), Unavailable},
		{"http 503", core.NewAPIError(503, nil, errors.New("down")), Unavailable},
		{"http 418", core.NewAPIError(418, nil, errors.New("teapot")), Err},

		// context
		{"deadline", context.DeadlineExceeded, Timeout},
		{"canceled", context.Canceled, Canceled},

		// cobra arg/flag parse errors (untyped strings)
		{"unknown flag", errors.New("unknown flag: --nope"), Usage},
		{"unknown command", errors.New("unknown command \"boop\" for \"axilio\""), Usage},
		{"flag needs arg", errors.New("flag needs an argument: --output"), Usage},
		{"accepts args", errors.New("accepts 1 arg(s), received 0"), Usage},
		{"requires args", errors.New("requires at least 1 arg(s), only received 0"), Usage},
		{"not a usage error", errors.New("something else failed"), Err},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.err); got != tc.want {
				t.Fatalf("Classify(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// The driver taxonomy must classify without a default-to-generic gap: every
// mobile.Code maps to something more specific than Err except the two we
// deliberately treat as generic.
func TestEveryDriverCodeMapped(t *testing.T) {
	specific := []mobile.Code{
		mobile.CodeUnauthorized, mobile.CodeInvalidArgs, mobile.CodeElementNotFound,
		mobile.CodeNoAllocation, mobile.CodeTimeout, mobile.CodeConnection,
		mobile.CodeNotConnected, mobile.CodeDeviceOffline, mobile.CodeCanceled,
	}
	for _, c := range specific {
		if got := fromMobile(c); got == Err {
			t.Fatalf("driver code %q classified as generic Err; expected a specific code", c)
		}
	}
}
