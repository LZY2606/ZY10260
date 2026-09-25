// Package web serves the offline CBOR forensic workbench.
package web

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"

	"cborbench/internal/cbor"
	"cborbench/internal/fixtures"
	"cborbench/internal/store"
)

type Server struct {
	store *store.Store
	tmpl  *template.Template
}

func New(st *store.Store) (*Server, error) {
	s := &Server{store: st}
	s.tmpl = template.Must(template.New("").Funcs(template.FuncMap{
		"hexStr":   func(b []byte) string { return hex.EncodeToString(b) },
		"hexBytes": func(b []byte) string { return prettyHex(b) },
		"dictate":  treeHTML,
	}).Parse(pageTmpl))
	return s, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/item", s.handleItem)
	mux.HandleFunc("/import", s.handleImport)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	items, err := s.store.ListItems()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, "index", map[string]any{"Items": items, "Fixtures": fixtures.All})
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	raw := strings.TrimSpace(r.FormValue("hex"))
	if name == "" {
		name = "manual"
	}
	data, err := parseHex(raw)
	if err != nil {
		s.render(w, "error", map[string]any{"Err": err.Error()})
		return
	}
	roots, ddiags := cbor.DecodeStream(data)
	res := cbor.Canonicalize(roots...)
	lenient := concatenate(roots)
	id, err := s.store.CreateItem(name, raw)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Three versions; original is insert-only and can never be overwritten.
	_ = s.store.PutVersion(id, store.KindOriginal, data, cbor.DigestBytes(data))
	_ = s.store.PutVersion(id, store.KindLenient, lenient, cbor.DigestBytes(lenient))
	_ = s.store.PutVersion(id, store.KindCanonical, res.Data, res.Digest)
	_ = ddiags
	http.Redirect(w, r, "/item?id="+fmt.Sprint(id), http.StatusSeeOther)
}

func concatenate(roots []*cbor.Node) []byte {
	var b bytes.Buffer
	for _, n := range roots {
		b.Write(cbor.EncodeLenient(n))
	}
	return b.Bytes()
}

func (s *Server) handleItem(w http.ResponseWriter, r *http.Request) {
	var id int64
	fmt.Sscan(r.URL.Query().Get("id"), &id)
	item, vers, err := s.store.GetItem(id)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	profile := r.URL.Query().Get("profile")
	if profile == "" {
		profile = "rfc8949-deterministic"
	}
	orig := vers[store.KindOriginal].Data
	roots, ddiags := cbor.DecodeStream(orig)
	res := cbor.Canonicalize(roots...)

	segments := topSegments(roots)
	dump := hexDump(orig, segments)
	svg := rangeSVG(len(orig), segments)
	tree := treeHTML(roots, ddiags)

	// second canonicalization over the canonical candidate (idempotency)
	reRoots, _ := cbor.DecodeStream(res.Data)
	re := cbor.Canonicalize(reRoots...)
	idempotent := bytes.Equal(re.Data, res.Data)

	digests := map[string]string{}
	lengths := map[string]int{}
	for k, v := range vers {
		digests[k] = v.Digest
		lengths[k] = len(v.Data)
	}
	s.render(w, "item", map[string]any{
		"Item":        item,
		"Vers":        vers,
		"Digests":     digests,
		"Lengths":     lengths,
		"DecodeDiags": ddiags,
		"Canon":       res,
		"Re":          re,
		"Idempotent":  idempotent,
		"Dump":        dump,
		"SVG":         template.HTML(svg),
		"Tree":        template.HTML(tree),
		"Profile":     profile,
	})
}

func (s *Server) render(w http.ResponseWriter, name string, data map[string]any) {
	data["View"] = name
	var buf bytes.Buffer
	if err := s.tmpl.Execute(&buf, data); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Write(buf.Bytes())
}

// Segment is one colored byte range for the hex dump and SVG.
type Segment struct {
	Start, End int
	Label      string
	Index      int
}

func topSegments(roots []*cbor.Node) []Segment {
	var segs []Segment
	add := func(start, end int, label string) {
		segs = append(segs, Segment{Start: start, End: end, Label: label, Index: len(segs)})
	}
	for _, n := range roots {
		switch n.Kind {
		case cbor.KindArray:
			for _, c := range n.Children {
				add(c.Start, c.End, c.Summary())
			}
		case cbor.KindMap:
			for i := range n.Keys {
				add(n.Keys[i].Start, n.Keys[i].End, "key "+n.Keys[i].Summary())
				add(n.Vals[i].Start, n.Vals[i].End, "val "+n.Vals[i].Summary())
			}
		case cbor.KindTag:
			if len(n.Children) == 1 {
				add(n.Children[0].Start, n.Children[0].End, n.Children[0].Summary())
			}
		default:
			add(n.Start, n.End, n.Summary())
		}
	}
	sort.SliceStable(segs, func(i, j int) bool { return segs[i].Start < segs[j].Start })
	return segs
}

func parseHex(s string) ([]byte, error) {
	s = strings.ReplaceAll(s, "0x", "")
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
			b.WriteRune(c)
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ':':
		default:
			return nil, fmt.Errorf("hex 中含非法字符 %q", c)
		}
	}
	return hex.DecodeString(b.String())
}

func prettyHex(b []byte) string {
	var sb strings.Builder
	for i, c := range b {
		if i > 0 && i%16 == 0 {
			sb.WriteByte('\n')
		}
		if i > 0 && i%16 != 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(fmt.Sprintf("%02x", c))
	}
	return sb.String()
}
