package orchestrator

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestDecodePatchOptions_Defaults(t *testing.T) {
	for _, raw := range []string{``, `null`, `{}`} {
		o, err := decodePatchOptions(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		want := PatchOptions{Scope: PatchScopeSecurity, BatchSize: 10, StopAfterFailures: 3}
		if !reflect.DeepEqual(o, want) {
			t.Errorf("%q = %+v, want %+v", raw, o, want)
		}
	}
}

func TestDecodePatchOptions_ZeroThresholdMeansNeverStop(t *testing.T) {
	o, err := decodePatchOptions(json.RawMessage(`{"stopAfterFailures":0}`))
	if err != nil || o.StopAfterFailures != 0 {
		t.Fatalf("got %+v, %v", o, err)
	}
}

func TestDecodePatchOptions_Packages(t *testing.T) {
	o, err := decodePatchOptions(json.RawMessage(`{"scope":"packages","packages":["curl"," openssl ","curl",""]}`))
	if err != nil || !reflect.DeepEqual(o.Packages, []string{"curl", "openssl"}) {
		t.Fatalf("got %+v, %v", o, err)
	}
}

func TestDecodePatchOptions_Refusals(t *testing.T) {
	cases := map[string]string{
		`{"scope":"everything"}`:                      "scope",
		`{"scope":"packages"}`:                        "packages",
		`{"scope":"all","packages":["curl"]}`:         "packages",
		`{"scope":"packages","packages":["--force"]}`: "packages",
		`{"scope":"packages","packages":["a;b"]}`:     "packages",
		`{"batchSize":0}`:                             "batchSize",
		`{"batchSize":101}`:                           "batchSize",
		`{"stopAfterFailures":-1}`:                    "stopAfterFailures",
		`{"stopAfterFailures":1001}`:                  "stopAfterFailures",
		`{"dryRun":true,"reboot":true}`:               "reboot",
		`{"unknown":1}`:                               "",
		`not json`:                                    "",
	}
	for raw, field := range cases {
		_, err := decodePatchOptions(json.RawMessage(raw))
		var bad *InvalidPatchOptionsError
		if !errors.As(err, &bad) {
			t.Errorf("%s: err = %v, want a refusal", raw, err)
			continue
		}
		if bad.Field != field {
			t.Errorf("%s: field = %q, want %q", raw, bad.Field, field)
		}
	}
}
