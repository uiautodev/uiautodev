package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestShouldPublishLatest(t *testing.T) {
	cases := []struct {
		name      string
		online    string
		newVer    string
		want      bool
		status    int
		badJSON   bool
		serverURL bool
	}{
		{name: "older online", online: "1.1.0", newVer: "1.2.0", want: true, status: http.StatusOK},
		{name: "same version", online: "1.2.0", newVer: "1.2.0", want: true, status: http.StatusOK},
		{name: "newer online", online: "1.3.0", newVer: "1.2.0", want: false, status: http.StatusOK},
		{name: "v prefix", online: "v1.3.0", newVer: "v1.2.0", want: false, status: http.StatusOK},
		{name: "server error", online: "1.3.0", newVer: "1.2.0", want: true, status: http.StatusInternalServerError},
		{name: "bad json", online: "", newVer: "1.2.0", want: true, status: http.StatusOK, badJSON: true},
		{name: "no url", online: "", newVer: "1.2.0", want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := ""
			if tc.name != "no url" {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if tc.badJSON {
						_, _ = w.Write([]byte("{not json"))
						return
					}
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(`{"version":"` + tc.online + `"}`))
				}))
				defer server.Close()
				url = server.URL
			}

			got, reason := shouldPublishLatest(context.Background(), http.DefaultClient, url, tc.newVer)
			if got != tc.want {
				t.Errorf("shouldPublishLatest() = %v (%s), want %v", got, reason, tc.want)
			}
			if reason == "" {
				t.Error("shouldPublishLatest() should explain its decision")
			}
		})
	}
}

func TestShouldPublishLatestUnreachable(t *testing.T) {
	got, reason := shouldPublishLatest(context.Background(), http.DefaultClient, "http://127.0.0.1:1/latest.json", "1.2.0")
	if !got {
		t.Errorf("unreachable guard should not block publishing, reason: %s", reason)
	}
}
