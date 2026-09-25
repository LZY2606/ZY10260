// Command server runs the offline CBOR forensic workbench.
package main

import (
	"flag"
	"log"
	"net/http"

	"cborbench/internal/cbor"
	"cborbench/internal/fixtures"
	"cborbench/internal/store"
	"cborbench/internal/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	dbPath := flag.String("db", "cborbench.db", "SQLite database path")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	defer st.Close()

	seed(st)

	srv, err := web.New(st)
	if err != nil {
		log.Fatalf("web: %v", err)
	}
	log.Printf("定序封签台 listening on http://%s", *addr)
	if err := http.ListenAndServe(*addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}

// seed loads the built-in corpus once, into an empty database.
func seed(st *store.Store) {
	n, err := st.CountItems()
	if err != nil || n > 0 {
		return
	}
	for _, f := range fixtures.All {
		data := f.Bytes()
		roots, _ := cbor.DecodeStream(data)
		res := cbor.Canonicalize(roots...)
		lenient := func() []byte {
			var b []byte
			for _, r := range roots {
				b = append(b, cbor.EncodeLenient(r)...)
			}
			return b
		}()
		id, err := st.CreateItem("fixture:"+f.Name, f.Hex)
		if err != nil {
			log.Printf("seed %s: %v", f.Name, err)
			continue
		}
		_ = st.PutVersion(id, store.KindOriginal, data, cbor.DigestBytes(data))
		_ = st.PutVersion(id, store.KindLenient, lenient, cbor.DigestBytes(lenient))
		_ = st.PutVersion(id, store.KindCanonical, res.Data, res.Digest)
	}
	log.Printf("seeded %d built-in fixtures", len(fixtures.All))
}
