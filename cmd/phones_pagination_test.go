package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	platformgo "github.com/axilioai/platform-go"
)

func TestPhonesInventoryFollowsPagesBeforeFiltering(t *testing.T) {
	for _, command := range []string{"mine", "list"} {
		t.Run(command, func(t *testing.T) {
			var offsets []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/api/v1/phones" || r.URL.Query().Get("ownership") != "dedicated" {
					t.Errorf("unexpected inventory request: %s %s", r.Method, r.URL)
				}
				offset := r.URL.Query().Get("offset")
				offsets = append(offsets, offset)
				var phones string
				switch offset {
				case "0":
					phones = `[{"phone_id":"busy","status":"active","current_session_id":"s1"},{"phone_id":"offline","status":"inactive"}]`
				case "2":
					// A short page can still have more results according to total.
					phones = `[{"phone_id":"target","status":"active"}]`
				case "3":
					phones = `[{"phone_id":"other","status":"active"}]`
				default:
					http.Error(w, "unexpected offset", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"phones":%s,"total":4,"limit":2,"offset":%s}`, phones, offset)
			}))
			t.Cleanup(srv.Close)
			out, err := run(t, srv, "-o", "json", "phones", command)
			if err != nil {
				t.Fatal(err)
			}
			var got platformgo.PhonePrivateListResponse
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, phone := range got.Phones {
				ids = append(ids, phone.PhoneID)
			}
			want := []string{"busy", "offline", "target", "other"}
			if command == "list" {
				want = []string{"target", "other"}
			}
			if !reflect.DeepEqual(ids, want) || got.Total != int64(len(want)) || got.Limit != int64(len(want)) || got.Offset != 0 {
				t.Fatalf("incomplete or inconsistent inventory: %s", out)
			}
			if !reflect.DeepEqual(offsets, []string{"0", "2", "3"}) {
				t.Fatalf("requested offsets %v", offsets)
			}
		})
	}
}

func TestPhonesInventoryDoesNotEmitPartialSuccess(t *testing.T) {
	for _, command := range []string{"mine", "list"} {
		for _, failure := range []string{"forbidden", "empty-page"} {
			t.Run(command+"/"+failure, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					switch {
					case r.URL.Query().Get("offset") == "0":
						_, _ = w.Write([]byte(`{"phones":[{"phone_id":"p1","status":"active"}],"total":2,"limit":1,"offset":0}`))
					case failure == "forbidden":
						http.Error(w, `{"message":"permission denied"}`, http.StatusForbidden)
					default:
						_, _ = w.Write([]byte(`{"phones":[],"total":2,"limit":1,"offset":1}`))
					}
				}))
				t.Cleanup(srv.Close)
				out, err := run(t, srv, "-o", "json", "phones", command)
				if err == nil || out != "" {
					t.Fatalf("partial inventory emitted as success: output=%q error=%v", out, err)
				}
			})
		}
	}
}
