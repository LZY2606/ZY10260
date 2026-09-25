package cbor

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Limits guarding every declared length before any allocation or read.
const (
	DefaultMaxDepth  = 64
	DefaultMaxItems  = 1 << 20
	DefaultMaxString = 1 << 24
)

// Decoder is a lenient, diagnostic-collecting CBOR decoder. It never
// mutates the input and records exact byte ranges for every node.
type Decoder struct {
	data     []byte
	pos      int
	depth    int
	maxDepth int
	budget   int
	shared   []*Node
	Diags    []Diagnostic
}

// NewDecoder builds a decoder over data with default limits.
func NewDecoder(data []byte) *Decoder {
	return &Decoder{data: data, maxDepth: DefaultMaxDepth, budget: DefaultMaxItems}
}

// Decode parses a single top-level item. Trailing bytes are reported.
func Decode(data []byte) (*Node, []Diagnostic) {
	d := NewDecoder(data)
	n := d.item()
	if d.pos < len(data) {
		d.diag("trailing-bytes", "warn", d.pos, len(data),
			fmt.Sprintf("%d trailing bytes after first item", len(data)-d.pos))
	}
	return n, d.Diags
}

// DecodeStream parses every top-level item in data.
func DecodeStream(data []byte) ([]*Node, []Diagnostic) {
	d := NewDecoder(data)
	var out []*Node
	for d.pos < len(data) {
		out = append(out, d.item())
	}
	return out, d.Diags
}

func (d *Decoder) diag(code, sev string, start, end int, msg string) {
	if end < start {
		end = start
	}
	d.Diags = append(d.Diags, Diagnostic{Code: code, Severity: sev, Msg: msg, Start: start, End: end})
}

func (d *Decoder) broken(start int, note string) *Node {
	return &Node{Kind: KindSimple, Simple: 255, Start: start, End: d.pos, Broken: true, Note: note}
}

// readArg reads the argument of an initial byte. ai are the low 5 bits.
func (d *Decoder) readArg(ai byte, start int) (val uint64, headLen int, indef, ok bool) {
	headLen = 1
	switch {
	case ai < 24:
		return uint64(ai), 1, false, true
	case ai == 24:
		if d.pos+1 > len(d.data) {
			d.diag("truncated", "error", start, len(d.data), "argument byte missing")
			return 0, 1, false, false
		}
		v := uint64(d.data[d.pos])
		d.pos++
		if v < 24 {
			d.diag("non-shortest", "info", start, d.pos,
				fmt.Sprintf("value %d encoded with 1-byte argument", v))
		}
		return v, 2, false, true
	case ai == 25:
		return d.readN(2, start)
	case ai == 26:
		return d.readN(4, start)
	case ai == 27:
		return d.readN(8, start)
	case ai == 31:
		return 0, 1, true, true
	default: // 28..30 reserved
		d.diag("reserved-ai", "error", start, d.pos, fmt.Sprintf("additional info %d is reserved", ai))
		return 0, 1, false, false
	}
}

func (d *Decoder) readN(n int, start int) (uint64, int, bool, bool) {
	if d.pos+n > len(d.data) {
		d.diag("truncated", "error", start, len(d.data),
			fmt.Sprintf("need %d argument bytes, %d remain", n, len(d.data)-d.pos))
		d.pos = len(d.data)
		return 0, 1 + len(d.data) - start, false, false
	}
	b := d.data[d.pos : d.pos+n]
	d.pos += n
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	// non-shortest check
	if v < 1<<uint(8*(n/2)) {
		d.diag("non-shortest", "info", start, d.pos,
			fmt.Sprintf("value %d encoded with %d-byte argument", v, n))
	}
	return v, 1 + n, false, true
}

func (d *Decoder) item() *Node {
	if d.budget <= 0 {
		d.diag("budget-exceeded", "error", d.pos, d.pos, "item budget exhausted")
		return d.broken(d.pos, "budget exhausted")
	}
	d.budget--
	if d.depth >= d.maxDepth {
		d.diag("depth-exceeded", "error", d.pos, d.pos, "nesting depth limit reached")
		return d.broken(d.pos, "depth limit")
	}
	start := d.pos
	if d.pos >= len(d.data) {
		d.diag("truncated", "error", start, start, "unexpected end of input")
		return d.broken(start, "unexpected end of input")
	}
	ib := d.data[d.pos]
	d.pos++
	major, ai := ib>>5, ib&0x1f

	if major == 7 {
		return d.simpleOrFloat(ai, start, ib)
	}

	arg, headLen, indef, ok := d.readArg(ai, start)
	n := &Node{Start: start, HeadLen: headLen, HeadAI: ai, Indef: indef}
	if !ok && !indef {
		n.End = d.pos
		n.Broken = true
		n.Note = "bad head"
		return n
	}

	switch major {
	case 0:
		n.Kind, n.Uint, n.End = KindUint, arg, d.pos
		return n
	case 1:
		n.Kind = KindNegInt
		if arg > math.MaxInt64 {
			n.Int = math.MinInt64
			d.diag("int-overflow", "warn", start, d.pos, "negative integer below int64 range")
		} else {
			n.Int = -1 - int64(arg)
		}
		n.End = d.pos
		return n
	case 2, 3:
		return d.string(n, major, arg, indef, start)
	case 4:
		return d.array(n, arg, indef, start)
	case 5:
		return d.mapping(n, arg, indef, start)
	case 6:
		return d.tag(n, arg, start)
	}
	n.End = d.pos
	n.Broken = true
	return n
}

func (d *Decoder) string(n *Node, major byte, arg uint64, indef bool, start int) *Node {
	isText := major == 3
	if isText {
		n.Kind = KindText
	} else {
		n.Kind = KindBytes
	}
	if !indef {
		if arg > DefaultMaxString {
			d.diag("budget-exceeded", "error", start, d.pos,
				fmt.Sprintf("declared string length %d exceeds budget", arg))
			n.End, n.Broken, n.Note = d.pos, true, "length over budget"
			return n
		}
		if arg > uint64(len(d.data)-d.pos) {
			d.diag("truncated", "error", start, len(d.data),
				fmt.Sprintf("string declares %d bytes, %d remain", arg, len(d.data)-d.pos))
			n.End, n.Broken, n.Note = len(d.data), true, "truncated string"
			d.pos = len(d.data)
			return n
		}
		payload := d.data[d.pos : d.pos+int(arg)]
		d.pos += int(arg)
		n.End = d.pos
		n.Raw = payload
		if isText {
			n.Text = string(payload)
		}
		return n
	}
	// indefinite-length: read definite chunks of the same major type
	var buf []byte
	for {
		if d.pos >= len(d.data) {
			d.diag("truncated", "error", start, d.pos, "indefinite string missing break")
			n.Broken, n.Note = true, "missing break"
			break
		}
		if d.data[d.pos] == 0xff {
			d.pos++
			break
		}
		cstart := d.pos
		cib := d.data[d.pos]
		cmajor, cai := cib>>5, cib&0x1f
		if cmajor != major || cai == 31 {
			d.diag("broken-chunk", "error", cstart, cstart+1,
				fmt.Sprintf("indefinite %s contains chunk of wrong type 0x%02x", stringKind(isText), cib))
			n.Broken, n.Note = true, "broken chunk"
			d.resync()
			break
		}
		d.pos++
		clen, chead, _, cok := d.readArg(cai, cstart)
		if !cok || clen > uint64(len(d.data)-d.pos) {
			d.diag("truncated", "error", cstart, len(d.data), "chunk truncated")
			n.Broken, n.Note = true, "chunk truncated"
			d.pos = len(d.data)
			break
		}
		chunk := &Node{Start: cstart, HeadLen: chead, HeadAI: cai, End: d.pos + int(clen)}
		payload := d.data[d.pos : d.pos+int(clen)]
		d.pos += int(clen)
		chunk.Kind, chunk.Raw = KindBytes, payload
		if isText {
			chunk.Kind = KindText
			chunk.Text = string(payload)
		}
		n.Children = append(n.Children, chunk)
		buf = append(buf, payload...)
		d.budget--
	}
	n.End = d.pos
	if n.Broken {
		n.Keep = d.data[start:d.pos]
	}
	n.Raw = buf
	if isText {
		n.Text = string(buf)
	}
	return n
}

// resync scans forward to the next break marker after a broken chunk so
// that siblings of the enclosing container survive.
func (d *Decoder) resync() {
	for d.pos < len(d.data) && d.data[d.pos] != 0xff {
		d.pos++
	}
	if d.pos < len(d.data) {
		d.pos++ // consume break
	}
}

func (d *Decoder) array(n *Node, arg uint64, indef bool, start int) *Node {
	n.Kind = KindArray
	d.depth++
	defer func() { d.depth--; n.End = d.pos }()
	if !indef {
		if arg > uint64(d.budget) || arg > DefaultMaxItems {
			d.diag("budget-exceeded", "error", start, d.pos,
				fmt.Sprintf("array declares %d items, budget %d", arg, d.budget))
			n.Broken, n.Note = true, "count over budget"
			return n
		}
		for i := uint64(0); i < arg; i++ {
			n.Children = append(n.Children, d.item())
		}
		return n
	}
	for {
		if d.pos >= len(d.data) {
			d.diag("truncated", "error", start, d.pos, "indefinite array missing break")
			n.Broken, n.Note = true, "missing break"
			return n
		}
		if d.data[d.pos] == 0xff {
			d.pos++
			return n
		}
		n.Children = append(n.Children, d.item())
	}
}

func (d *Decoder) mapping(n *Node, arg uint64, indef bool, start int) *Node {
	n.Kind = KindMap
	d.depth++
	defer func() { d.depth--; n.End = d.pos }()
	pair := func() bool {
		k := d.item()
		v := d.item()
		n.Keys = append(n.Keys, k)
		n.Vals = append(n.Vals, v)
		return true
	}
	if !indef {
		if arg > uint64(d.budget) || arg > DefaultMaxItems {
			d.diag("budget-exceeded", "error", start, d.pos,
				fmt.Sprintf("map declares %d pairs, budget %d", arg, d.budget))
			n.Broken, n.Note = true, "count over budget"
			return n
		}
		for i := uint64(0); i < arg; i++ {
			pair()
		}
		return n
	}
	for {
		if d.pos >= len(d.data) {
			d.diag("truncated", "error", start, d.pos, "indefinite map missing break")
			n.Broken, n.Note = true, "missing break"
			return n
		}
		if d.data[d.pos] == 0xff {
			d.pos++
			return n
		}
		pair()
	}
}

func (d *Decoder) tag(n *Node, num uint64, start int) *Node {
	d.depth++
	defer func() { d.depth--; n.End = d.pos }()
	switch num {
	case 28: // shareable: register before decoding content so cycles can form
		n.Kind = KindTag
		n.TagNum = 28
		n.ShareIdx = len(d.shared)
		d.shared = append(d.shared, n)
		n.Children = []*Node{d.item()}
		return n
	case 29: // sharedref: content is an integer index
		n.Kind = KindSharedRef
		child := d.item()
		n.Children = []*Node{child}
		if child.Kind == KindUint {
			n.RefIdx = child.Uint
			if int(child.Uint) < len(d.shared) {
				n.RefTarget = d.shared[child.Uint]
			} else {
				d.diag("undefined-ref", "error", start, d.pos,
					fmt.Sprintf("shared reference #%d has no matching shareable value", child.Uint))
			}
		} else {
			d.diag("bad-ref", "error", start, d.pos, "shared reference index is not an unsigned integer")
		}
		return n
	default:
		n.Kind = KindTag
		n.TagNum = num
		n.ShareIdx = -1
		n.Children = []*Node{d.item()}
		d.diag("unknown-tag", "info", start, d.pos,
			fmt.Sprintf("tag %d preserved without interpretation", num))
		return n
	}
}

func (d *Decoder) simpleOrFloat(ai byte, start int, ib byte) *Node {
	n := &Node{Kind: KindSimple, Start: start, HeadLen: 1, HeadAI: ai}
	switch {
	case ai < 24:
		n.Simple = ai
		n.End = d.pos
	case ai == 24:
		if d.pos+1 > len(d.data) {
			d.diag("truncated", "error", start, len(d.data), "simple value byte missing")
			n.End, n.Broken = d.pos, true
			return n
		}
		v := d.data[d.pos]
		d.pos++
		n.HeadLen = 2
		n.Simple = v
		n.End = d.pos
		if v < 32 {
			d.diag("reserved-simple", "warn", start, d.pos,
				fmt.Sprintf("simple(%d) uses reserved one-byte form", v))
		}
	case ai == 25, ai == 26, ai == 27:
		w := 1 << (ai - 24) // ai 25,26,27 -> 2,4,8
		if d.pos+w > len(d.data) {
			d.diag("truncated", "error", start, len(d.data), "float bits truncated")
			n.End, n.Broken = len(d.data), true
			d.pos = len(d.data)
			return n
		}
		n.Kind = KindFloat
		n.Width = w
		n.HeadLen = 1 + w
		b := d.data[d.pos : d.pos+w]
		d.pos += w
		n.End = d.pos
		switch w {
		case 2:
			n.Bits = uint64(binary.BigEndian.Uint16(b))
			n.Float = halfToFloat(uint16(n.Bits))
		case 4:
			n.Bits = uint64(binary.BigEndian.Uint32(b))
			n.Float = float64(math.Float32frombits(uint32(n.Bits)))
		case 8:
			n.Bits = binary.BigEndian.Uint64(b)
			n.Float = math.Float64frombits(n.Bits)
		}
		d.floatDiags(n)
	case ai == 31:
		d.diag("stray-break", "error", start, d.pos, "break marker outside indefinite container")
		n.Broken = true
		n.Note = "stray break"
		n.End = d.pos
	default:
		d.diag("reserved-ai", "error", start, d.pos, fmt.Sprintf("additional info %d reserved", ai))
		n.Broken = true
		n.End = d.pos
	}
	return n
}

func (d *Decoder) floatDiags(n *Node) {
	if math.IsNaN(n.Float) {
		canonical := uint64(0x7e00)
		switch n.Width {
		case 4:
			canonical = 0x7fc00000
		case 8:
			canonical = 0x7ff8000000000000
		}
		if n.Bits != canonical {
			d.diag("nan-payload", "warn", n.Start, n.End,
				fmt.Sprintf("NaN carries non-canonical payload bits 0x%x", n.Bits))
		}
	}
	if n.Float == 0 && math.Signbit(n.Float) {
		d.diag("neg-zero", "info", n.Start, n.End, "negative zero")
	}
}

func stringKind(text bool) string {
	if text {
		return "text string"
	}
	return "byte string"
}
