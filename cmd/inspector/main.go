// inspector is a simple HTTP server that prints every incoming request —
// method, URL, headers, and body — to stdout. Useful for inspecting what
// portier (or any HTTP client) actually sends.
//
// Usage:
//
//	go run ./cmd/inspector          # listens on :9000
//	go run ./cmd/inspector :8888    # custom address
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
)

func main() {
	addr := ":9000"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}

	http.HandleFunc("/", handle)
	log.Printf("inspector listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func handle(w http.ResponseWriter, r *http.Request) {
	var sb strings.Builder

	fmt.Fprintf(&sb, "\n──────────────────────────────────────────\n")
	fmt.Fprintf(&sb, "%s %s %s\n", r.Method, r.RequestURI, r.Proto)

	// Headers — sorted for stable output
	keys := make([]string, 0, len(r.Header))
	for k := range r.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range r.Header[k] {
			fmt.Fprintf(&sb, "%s: %s\n", k, v)
		}
	}

	// Body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		fmt.Fprintf(&sb, "\n[error reading body: %v]\n", err)
	} else if len(body) > 0 {
		fmt.Fprintf(&sb, "\n%s\n", body)
	}

	fmt.Fprintf(&sb, "──────────────────────────────────────────\n")
	fmt.Print(sb.String())

	w.WriteHeader(http.StatusOK)
}
