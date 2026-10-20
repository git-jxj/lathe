package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestPaginateAll_CursorEmptyPages(t *testing.T) {
	for _, strategy := range []string{"cursor", "body-cursor"} {
		for _, tc := range []struct {
			name     string
			pages    [][]int
			maxPages int
			want     []int
			requests int32
		}{
			{name: "empty first and intermediate pages", pages: [][]int{{}, {1}, {}, {2}, {}}, maxPages: 10, want: []int{1, 2}, requests: 5},
			{name: "empty terminal page", pages: [][]int{{}}, maxPages: 10, requests: 1},
			{name: "empty pages obey request cap", pages: [][]int{{}, {}, {}}, maxPages: 2, requests: 2},
		} {
			t.Run(strategy+"/"+tc.name, func(t *testing.T) {
				var calls atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					token := r.URL.Query().Get("page_token")
					if strategy == "body-cursor" {
						var body struct {
							Variables struct {
								After string `json:"after"`
							} `json:"variables"`
						}
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Errorf("decode pagination body: %v", err)
							http.Error(w, "invalid body", http.StatusBadRequest)
							return
						}
						token = body.Variables.After
					}
					page := 0
					if token != "" {
						var err error
						page, err = strconv.Atoi(token)
						if err != nil || page < 0 || page >= len(tc.pages) {
							t.Errorf("unexpected pagination token %q", token)
							http.Error(w, "invalid token", http.StatusBadRequest)
							return
						}
					}
					calls.Add(1)
					next := ""
					if page+1 < len(tc.pages) {
						next = strconv.Itoa(page + 1)
					}
					response := map[string]any{"items": tc.pages[page], "next_page_token": next}
					response["pageInfo"] = map[string]any{"endCursor": next, "hasNextPage": next != ""}
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(response); err != nil {
						t.Errorf("encode pagination response: %v", err)
					}
				}))
				defer srv.Close()

				method := http.MethodGet
				var body any
				hint := PaginationHint{Strategy: strategy, TokenParam: "page_token", TokenField: "next_page_token"}
				if strategy == "body-cursor" {
					method = http.MethodPost
					body = []byte(`{"variables":{}}`)
					hint.TokenParam = "variables.after"
					hint.TokenField = "pageInfo.endCursor"
				}
				data, err := PaginateAll(context.Background(), srv.URL, method, "/items", body, ClientOptions{Timeout: 5 * time.Second}, hint, "items", tc.maxPages)
				if err != nil {
					t.Fatal(err)
				}
				var result struct {
					Items []int `json:"items"`
				}
				if err := json.Unmarshal(data, &result); err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(result.Items, tc.want) {
					t.Errorf("items = %v, want %v", result.Items, tc.want)
				}
				if got := calls.Load(); got != tc.requests {
					t.Errorf("requests = %d, want %d", got, tc.requests)
				}
			})
		}
	}
}
