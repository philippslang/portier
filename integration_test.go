// Integration tests for the MCP gateway
package portier

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// captured holds the details of the last HTTP request received by the stub server.
type captured struct {
	method      string
	path        string
	rawQuery    string
	body        []byte
	contentType string
	called      bool
}

// newStub starts an httptest.Server that records each inbound request into cap
// and always responds HTTP 200 with an empty JSON object.
func newStub(t *testing.T) (*httptest.Server, *captured) {
	t.Helper()
	cap := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*cap = captured{
			method:      r.Method,
			path:        r.URL.Path,
			rawQuery:    r.URL.RawQuery,
			body:        body,
			contentType: r.Header.Get("Content-Type"),
			called:      true,
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "{}")
	}))
	return srv, cap
}

// newTestRegistry creates a Registry pointing both bundled specs at the stub server.
// RequireConfirmation is set to true on both services so the write gate is active.
func newTestRegistry(t *testing.T, stub *httptest.Server) *Registry {
	t.Helper()
	trueVal := true
	reg := NewRegistry(stub.Client())
	for _, cfg := range []ServiceConfig{
		{Name: "pets", SpecPath: "apis/pets.yaml", Host: stub.URL, RequireConfirmation: &trueVal},
		{Name: "bookstore", SpecPath: "apis/bookstore.yaml", Host: stub.URL, RequireConfirmation: &trueVal},
	} {
		if err := reg.LoadSpec(cfg); err != nil {
			t.Fatalf("LoadSpec %s: %v", cfg.Name, err)
		}
	}
	return reg
}

// callCase is a single TestCallOperation test case.
type callCase struct {
	name        string
	service     string
	operationID string
	params      map[string]any
	confirmed   bool
	wantMethod  string
	wantPath    string
	wantQuery   string
	wantBody    map[string]any
	wantBlocked bool
}

// opCase is a single TestListOperations test case.
type opCase struct {
	name         string
	service      string
	tag          string
	operationID  string
	wantOpIDs    []string
	wantField    string
	wantRequired []string
}

// TestCallOperation verifies that call_operation translates tool invocations into
// the correct outbound HTTP requests, and that the write gate blocks mutating
// operations when confirmed=false.
func TestCallOperation(t *testing.T) {
	stub, cap := newStub(t)
	defer stub.Close()
	reg := newTestRegistry(t, stub)

	cases := []callCase{
		// ── GET operations (US1) ──────────────────────────────────────────────

		// pets
		// Paths include /v1 because the spec server URL is https://petstore.example.com/v1
		// and overriding Host preserves the spec's base path.
		{
			name: "listPets", service: "pets", operationID: "listPets",
			wantMethod: "GET", wantPath: "/v1/pets",
		},
		{
			name: "listPets with limit", service: "pets", operationID: "listPets",
			params:     map[string]any{"limit": 5},
			wantMethod: "GET", wantPath: "/v1/pets", wantQuery: "limit=5",
		},
		{
			name: "listPets omit limit", service: "pets", operationID: "listPets",
			wantMethod: "GET", wantPath: "/v1/pets", wantQuery: "",
		},
		{
			name: "getPetById", service: "pets", operationID: "getPetById",
			params:     map[string]any{"petId": "pet-42"},
			wantMethod: "GET", wantPath: "/v1/pets/pet-42",
		},

		// bookstore
		{
			name: "listBooks", service: "bookstore", operationID: "listBooks",
			wantMethod: "GET", wantPath: "/v1/books",
		},
		{
			name: "listBooks with limit", service: "bookstore", operationID: "listBooks",
			params:     map[string]any{"limit": 3},
			wantMethod: "GET", wantPath: "/v1/books", wantQuery: "limit=3",
		},
		{
			name: "listBooks with genre", service: "bookstore", operationID: "listBooks",
			params:     map[string]any{"genre": "fiction"},
			wantMethod: "GET", wantPath: "/v1/books", wantQuery: "genre=fiction",
		},
		{
			name: "listBooks omit params", service: "bookstore", operationID: "listBooks",
			wantMethod: "GET", wantPath: "/v1/books", wantQuery: "",
		},
		{
			name: "getBookById", service: "bookstore", operationID: "getBookById",
			params:     map[string]any{"bookId": "bk-7"},
			wantMethod: "GET", wantPath: "/v1/books/bk-7",
		},
		{
			name: "listReviews", service: "bookstore", operationID: "listReviews",
			params:     map[string]any{"bookId": "bk-7"},
			wantMethod: "GET", wantPath: "/v1/books/bk-7/reviews",
		},

		// ── Mutating operations with confirmed=true (US2) ─────────────────────

		// pets
		{
			name: "createPet confirmed", service: "pets", operationID: "createPet",
			params: map[string]any{"name": "Fido"}, confirmed: true,
			wantMethod: "POST", wantPath: "/v1/pets",
			wantBody:   map[string]any{"name": "Fido"},
		},
		{
			// updatePet uses PATCH per the OpenAPI spec (pets.yaml line 65).
			name: "updatePet confirmed", service: "pets", operationID: "updatePet",
			params: map[string]any{"petId": "pet-42", "name": "Rex"}, confirmed: true,
			wantMethod: "PATCH", wantPath: "/v1/pets/pet-42",
			wantBody:   map[string]any{"name": "Rex"},
		},
		{
			name: "deletePet confirmed", service: "pets", operationID: "deletePet",
			params: map[string]any{"petId": "pet-42"}, confirmed: true,
			wantMethod: "DELETE", wantPath: "/v1/pets/pet-42",
		},

		// bookstore
		{
			name: "createBook confirmed", service: "bookstore", operationID: "createBook",
			params: map[string]any{"title": "Dune", "author": "Herbert"}, confirmed: true,
			wantMethod: "POST", wantPath: "/v1/books",
			wantBody:   map[string]any{"title": "Dune", "author": "Herbert"},
		},
		{
			name: "replaceBook confirmed", service: "bookstore", operationID: "replaceBook",
			params: map[string]any{"bookId": "bk-7", "title": "Dune Messiah", "author": "Herbert"}, confirmed: true,
			wantMethod: "PUT", wantPath: "/v1/books/bk-7",
			wantBody:   map[string]any{"title": "Dune Messiah", "author": "Herbert"},
		},
		{
			// NOTE: Content-Type is asserted as application/json (current behaviour).
			// patchBook declares application/json-patch+json in the spec; aligning
			// the Content-Type header to the spec's declared media type is a known gap.
			name: "patchBook confirmed", service: "bookstore", operationID: "patchBook",
			params: map[string]any{"bookId": "bk-7", "price": 14.99}, confirmed: true,
			wantMethod: "PATCH", wantPath: "/v1/books/bk-7",
			wantBody:   map[string]any{"price": 14.99},
		},
		{
			name: "deleteBook confirmed", service: "bookstore", operationID: "deleteBook",
			params: map[string]any{"bookId": "bk-7"}, confirmed: true,
			wantMethod: "DELETE", wantPath: "/v1/books/bk-7",
		},
		{
			name: "createReview confirmed", service: "bookstore", operationID: "createReview",
			params: map[string]any{"bookId": "bk-7", "rating": 5, "body": "Great read"}, confirmed: true,
			wantMethod: "POST", wantPath: "/v1/books/bk-7/reviews",
		},

		// ── Write gate blocked (US2) ───────────────────────────────────────────

		{name: "createPet blocked", service: "pets", operationID: "createPet",
			params: map[string]any{"name": "Fido"}, wantBlocked: true},
		{name: "updatePet blocked", service: "pets", operationID: "updatePet",
			params: map[string]any{"petId": "pet-42"}, wantBlocked: true},
		{name: "deletePet blocked", service: "pets", operationID: "deletePet",
			params: map[string]any{"petId": "pet-42"}, wantBlocked: true},
		{name: "createBook blocked", service: "bookstore", operationID: "createBook",
			params: map[string]any{"title": "Dune"}, wantBlocked: true},
		{name: "replaceBook blocked", service: "bookstore", operationID: "replaceBook",
			params: map[string]any{"bookId": "bk-7"}, wantBlocked: true},
		{name: "patchBook blocked", service: "bookstore", operationID: "patchBook",
			params: map[string]any{"bookId": "bk-7"}, wantBlocked: true},
		{name: "deleteBook blocked", service: "bookstore", operationID: "deleteBook",
			params: map[string]any{"bookId": "bk-7"}, wantBlocked: true},
		{name: "createReview blocked", service: "bookstore", operationID: "createReview",
			params: map[string]any{"bookId": "bk-7"}, wantBlocked: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			*cap = captured{} // reset between subtests

			// Copy params so CallOperation's in-place deletions don't affect table rows.
			params := make(map[string]any, len(tc.params))
			for k, v := range tc.params {
				params[k] = v
			}

			result, err := reg.CallOperation(tc.service, tc.operationID, params, tc.confirmed)
			if err != nil {
				t.Fatalf("CallOperation: %v", err)
			}

			if tc.wantBlocked {
				if cap.called {
					t.Error("expected no HTTP request (write gate should block), but stub was called")
				}
				if status, _ := result["status"].(string); status != "confirmation_required" {
					t.Errorf("result[status] = %q, want %q", status, "confirmation_required")
				}
				return
			}

			if !cap.called {
				t.Fatal("expected HTTP request to stub, but stub was not called")
			}
			if cap.method != tc.wantMethod {
				t.Errorf("method = %q, want %q", cap.method, tc.wantMethod)
			}
			if cap.path != tc.wantPath {
				t.Errorf("path = %q, want %q", cap.path, tc.wantPath)
			}
			if cap.rawQuery != tc.wantQuery {
				t.Errorf("query = %q, want %q", cap.rawQuery, tc.wantQuery)
			}
			if tc.wantBody != nil {
				var got map[string]any
				if err := json.Unmarshal(cap.body, &got); err != nil {
					t.Fatalf("unmarshal body: %v", err)
				}
				if !reflect.DeepEqual(got, tc.wantBody) {
					t.Errorf("body = %v, want %v", got, tc.wantBody)
				}
				if !strings.HasPrefix(cap.contentType, "application/json") {
					t.Errorf("Content-Type = %q, want application/json prefix", cap.contentType)
				}
			}
		})
	}
}

// TestListServices verifies that list_services returns both registered services.
func TestListServices(t *testing.T) {
	stub, _ := newStub(t)
	defer stub.Close()
	reg := newTestRegistry(t, stub)

	services := reg.ListServices()
	names := make([]string, 0, len(services))
	for _, svc := range services {
		name, _ := svc["name"].(string)
		names = append(names, name)
	}
	sort.Strings(names)

	want := []string{"bookstore", "pets"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("service names = %v, want %v", names, want)
	}
}

// TestListOperations verifies that list_operations returns the correct operation IDs
// and that the tag filter correctly scopes results.
func TestListOperations(t *testing.T) {
	stub, _ := newStub(t)
	defer stub.Close()
	reg := newTestRegistry(t, stub)

	cases := []struct {
		name      string
		service   string
		tag       string
		wantOpIDs []string
	}{
		{
			name:      "pets no filter",
			service:   "pets",
			wantOpIDs: []string{"createPet", "deletePet", "getPetById", "listPets", "updatePet"},
		},
		{
			name:      "bookstore tag books",
			service:   "bookstore",
			tag:       "books",
			wantOpIDs: []string{"createBook", "deleteBook", "getBookById", "listBooks", "patchBook", "replaceBook"},
		},
		{
			name:      "bookstore tag reviews",
			service:   "bookstore",
			tag:       "reviews",
			wantOpIDs: []string{"createReview", "listReviews"},
		},
		{
			name:    "bookstore no filter",
			service: "bookstore",
			wantOpIDs: []string{
				"createBook", "createReview", "deleteBook", "getBookById",
				"listBooks", "listReviews", "patchBook", "replaceBook",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ops, err := reg.ListOperations(tc.service, tc.tag)
			if err != nil {
				t.Fatalf("ListOperations: %v", err)
			}
			got := make([]string, 0, len(ops))
			for _, op := range ops {
				got = append(got, op["operationId"].(string))
			}
			sort.Strings(got)
			if !reflect.DeepEqual(got, tc.wantOpIDs) {
				t.Errorf("operationIds = %v, want %v", got, tc.wantOpIDs)
			}
		})
	}
}

// TestGetOperationDetail verifies that get_operation_detail returns correct metadata
// including parameters, request body schema, response schema, and confirmation flag.
func TestGetOperationDetail(t *testing.T) {
	stub, _ := newStub(t)
	defer stub.Close()
	reg := newTestRegistry(t, stub)

	// findParam scans a parameters slice for an entry with the given name.
	findParam := func(params []map[string]any, name string) map[string]any {
		for _, p := range params {
			if p["name"] == name {
				return p
			}
		}
		return nil
	}

	t.Run("createPet has requestBody", func(t *testing.T) {
		detail, err := reg.GetOperationDetail("pets", "createPet")
		if err != nil {
			t.Fatalf("GetOperationDetail: %v", err)
		}
		if _, ok := detail["requestBody"]; !ok {
			t.Error("expected requestBody key in detail, not found")
		}
	})

	t.Run("listPets has optional limit param", func(t *testing.T) {
		detail, err := reg.GetOperationDetail("pets", "listPets")
		if err != nil {
			t.Fatalf("GetOperationDetail: %v", err)
		}
		params, _ := detail["parameters"].([]map[string]any)
		p := findParam(params, "limit")
		if p == nil {
			t.Fatal("limit parameter not found in listPets")
		}
		if p["required"] != false {
			t.Errorf("limit required = %v, want false", p["required"])
		}
	})

	t.Run("getPetById has required petId param", func(t *testing.T) {
		detail, err := reg.GetOperationDetail("pets", "getPetById")
		if err != nil {
			t.Fatalf("GetOperationDetail: %v", err)
		}
		params, _ := detail["parameters"].([]map[string]any)
		p := findParam(params, "petId")
		if p == nil {
			t.Fatal("petId parameter not found in getPetById")
		}
		if p["required"] != true {
			t.Errorf("petId required = %v, want true", p["required"])
		}
	})

	t.Run("getPetById has responseSchema", func(t *testing.T) {
		detail, err := reg.GetOperationDetail("pets", "getPetById")
		if err != nil {
			t.Fatalf("GetOperationDetail: %v", err)
		}
		if _, ok := detail["responseSchema"]; !ok {
			t.Error("expected responseSchema key in detail, not found")
		}
	})

	t.Run("createBook has requestBody", func(t *testing.T) {
		detail, err := reg.GetOperationDetail("bookstore", "createBook")
		if err != nil {
			t.Fatalf("GetOperationDetail: %v", err)
		}
		if _, ok := detail["requestBody"]; !ok {
			t.Error("expected requestBody key in detail, not found")
		}
	})

	t.Run("patchBook confirmationRequired true", func(t *testing.T) {
		detail, err := reg.GetOperationDetail("bookstore", "patchBook")
		if err != nil {
			t.Fatalf("GetOperationDetail: %v", err)
		}
		if detail["confirmationRequired"] != true {
			t.Errorf("confirmationRequired = %v, want true", detail["confirmationRequired"])
		}
	})

	t.Run("listPets confirmationRequired false", func(t *testing.T) {
		detail, err := reg.GetOperationDetail("pets", "listPets")
		if err != nil {
			t.Fatalf("GetOperationDetail: %v", err)
		}
		if detail["confirmationRequired"] != false {
			t.Errorf("confirmationRequired = %v, want false", detail["confirmationRequired"])
		}
	})
}
