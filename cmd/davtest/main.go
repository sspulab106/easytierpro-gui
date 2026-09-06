// Command davtest is a throwaway WebDAV server used to verify the GUI's
// cloud-sync feature end to end without touching a real cloud drive:
//
//	go run ./cmd/davtest -addr 127.0.0.1:9800 -dir <dir> -user test -pass test123
//
// It serves a full WebDAV tree (PUT/GET/MKCOL/PROPFIND/DELETE) with Basic
// auth over the golang.org/x/net/webdav handler.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"golang.org/x/net/webdav"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9800", "listen address")
	dir := flag.String("dir", "./davroot", "root directory to serve")
	user := flag.String("user", "test", "basic-auth username")
	pass := flag.String("pass", "test123", "basic-auth password")
	flag.Parse()

	if err := os.MkdirAll(*dir, 0o755); err != nil {
		log.Fatal(err)
	}
	h := &webdav.Handler{
		FileSystem: webdav.Dir(*dir),
		LockSystem: webdav.NewMemLS(),
	}
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			if !ok || u != *user || p != *pass {
				w.Header().Set("WWW-Authenticate", `Basic realm="davtest"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	log.Printf("davtest serving %s on http://%s (user=%s)", *dir, *addr, *user)
	log.Fatal(http.ListenAndServe(*addr, auth(h)))
}
