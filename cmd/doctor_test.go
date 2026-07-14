package cmd

import (
	"testing"

	"github.com/axilioai/cli/internal/exit"
)

func TestDoctorResultOK(t *testing.T) {
	cases := []struct {
		name   string
		checks []check
		wantOK bool
	}{
		{"all ok", []check{{Name: "A", Status: statusOK, required: true}}, true},
		{"warn is not a failure", []check{{Name: "A", Status: statusWarn, required: true}}, true},
		{"non-required fail is ignored", []check{{Name: "A", Status: statusFail}}, true},
		{"required fail flips ok", []check{{Name: "A", Status: statusFail, required: true}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := doctorResult(tc.checks)["ok"].(bool)
			if got != tc.wantOK {
				t.Fatalf("doctorResult ok = %v, want %v", got, tc.wantOK)
			}
		})
	}
}

func TestWorstFailure(t *testing.T) {
	cases := []struct {
		name     string
		checks   []check
		wantCode exit.Code
		wantName string // "" means no failure
	}{
		{
			"no failures",
			[]check{{Name: "Connectivity", Status: statusOK, required: true}},
			exit.OK, "",
		},
		{
			"credentials outrank connectivity",
			[]check{
				{Name: "Connectivity", Status: statusFail, required: true},
				{Name: "Credentials", Status: statusFail, required: true},
			},
			exit.Auth, "Credentials",
		},
		{
			"authentication is an auth failure",
			[]check{{Name: "Authentication", Status: statusFail, required: true}},
			exit.Auth, "Authentication",
		},
		{
			"connectivity alone is unavailable",
			[]check{{Name: "Connectivity", Status: statusFail, required: true}},
			exit.Unavailable, "Connectivity",
		},
		{
			"a non-required fail does not gate",
			[]check{{Name: "Account", Status: statusFail}},
			exit.OK, "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, failed := worstFailure(tc.checks)
			if code != tc.wantCode {
				t.Fatalf("code = %d, want %d", code, tc.wantCode)
			}
			if tc.wantName == "" {
				if failed != nil {
					t.Fatalf("expected no failure, got %q", failed.Name)
				}
				return
			}
			if failed == nil || failed.Name != tc.wantName {
				t.Fatalf("failed = %v, want %q", failed, tc.wantName)
			}
		})
	}
}
