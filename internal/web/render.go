package web

import (
	"fmt"
	"html"
	"strings"

	"cborbench/internal/cbor"
)

type DumpCell struct {
	Byte  string
	Color string // precomputed, -1 segments get neutral background
}
type DumpRow struct {
	Offset int
	Cells  []DumpCell
}

var palette = []string{"#9ec5fe", "#a3cfbb", "#fdd0a2", "#f5b7b1", "#d2b4de",
	"#a9dfbf", "#f9e79f", "#aed6f1", "#f5cba7", "#d7bde2", "#abebc6", "#f7dc6f"}

func segColor(i int) string {
	if i < 0 {
		return "#eceff4"
	}
	return palette[i%len(palette)]
}

func hexDump(data []byte, segs []Segment) []DumpRow {
	segAt := make([]int, len(data))
	for i := range segAt {
		segAt[i] = -1
	}
	for _, s := range segs {
		for i := s.Start; i < s.End && i < len(data); i++ {
			segAt[i] = s.Index
		}
	}
	var rows []DumpRow
	for off := 0; off < len(data); off += 16 {
		row := DumpRow{Offset: off}
		for i := 0; i < 16 && off+i < len(data); i++ {
			row.Cells = append(row.Cells, DumpCell{
				Byte:  fmt.Sprintf("%02x", data[off+i]),
				Color: segColor(segAt[off+i]),
			})
		}
		rows = append(rows, row)
	}
	return rows
}

// rangeSVG draws the encoded byte ranges as a horizontal segmented bar.
func rangeSVG(total int, segs []Segment) string {
	if total <= 0 {
		return `<svg xmlns="http://www.w3.org/2000/svg" width="900" height="40"></svg>`
	}
	const W = 900
	barH := 34
	y := 8
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" role="img" aria-label="byte ranges">`, W, barH+18+16*len(segs))
	fmt.Fprintf(&b, `<rect x="0" y="0" width="%d" height="%d" fill="#fff"/>`, W, barH+18+16*len(segs))
	scale := func(off int) int { return off * W / total }
	for _, s := range segs {
		x := scale(s.Start)
		w := scale(s.End) - x
		if w < 1 {
			w = 1
		}
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" fill="%s" stroke="#444" stroke-width="0.5"/>`,
			x, y, w, barH, segColor(s.Index))
		cx := x + w/2
		if w >= 34 {
			fmt.Fprintf(&b, `<text x="%d" y="%d" font-size="9" text-anchor="middle" fill="#222">%d–%d</text>`,
				cx, y+20, s.Start, s.End)
		}
	}
	fmt.Fprintf(&b, `<text x="0" y="%d" font-size="10" fill="#666">0</text>`, barH+16)
	fmt.Fprintf(&b, `<text x="%d" y="%d" font-size="10" text-anchor="end" fill="#666">%d</text>`, W, barH+16, total)
	for i, s := range segs {
		ly := barH + 30 + 16*i
		fmt.Fprintf(&b, `<rect x="0" y="%d" width="10" height="10" fill="%s" stroke="#444" stroke-width="0.5"/>`,
			ly-9, segColor(s.Index))
		fmt.Fprintf(&b, `<text x="16" y="%d" font-size="11" fill="#222">[%d,%d) %s</text>`,
			ly, s.Start, s.End, html.EscapeString(s.Label))
	}
	b.WriteString(`</svg>`)
	return b.String()
}

// treeHTML renders the parse tree with exact byte ranges and diagnostics.
func treeHTML(roots []*cbor.Node, diags []cbor.Diagnostic) string {
	byRange := map[string][]cbor.Diagnostic{}
	for _, dg := range diags {
		k := fmt.Sprintf("%d:%d", dg.Start, dg.End)
		byRange[k] = append(byRange[k], dg)
	}
	var b strings.Builder
	b.WriteString(`<ul class="tree">`)
	for _, n := range roots {
		renderNode(&b, n, byRange)
	}
	b.WriteString(`</ul>`)
	return b.String()
}

func renderNode(b *strings.Builder, n *cbor.Node, diags map[string][]cbor.Diagnostic) {
	attrs := ` class="node"`
	if n.Broken {
		attrs = ` class="node broken"`
	}
	fmt.Fprintf(b, `<li%s>`, attrs)
	fmt.Fprintf(b, `<span class="range">[%d,%d) head %d</span> %s <code>%s</code>`,
		n.Start, n.End, n.HeadLen, badge(n), html.EscapeString(n.Summary()))
	if n.Indef {
		b.WriteString(` <em>indef</em>`)
	}
	for _, dg := range diags[fmt.Sprintf("%d:%d", n.Start, n.End)] {
		fmt.Fprintf(b, ` <span class="diag %s" title="%s">%s</span>`,
			dg.Severity, html.EscapeString(dg.Msg), html.EscapeString(dg.Code))
	}
	kids := n.Children
	if len(kids) > 0 || len(n.Keys) > 0 {
		b.WriteString(`<ul>`)
		for _, c := range kids {
			renderNode(b, c, diags)
		}
		for i := range n.Keys {
			renderNode(b, n.Keys[i], diags)
			renderNode(b, n.Vals[i], diags)
		}
		b.WriteString(`</ul>`)
	}
	b.WriteString(`</li>`)
}

func badge(n *cbor.Node) string {
	return `<span class="kind">` + n.Kind.String() + `</span>`
}
