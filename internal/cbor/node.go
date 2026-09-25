// Package cbor implements a forensic, byte-preserving CBOR decoder,
// a lossless lenient re-encoder and an RFC 8949 deterministic encoder.
package cbor

import "fmt"

// Kind identifies the semantic type of a decoded node.
type Kind int

const (
	KindUint Kind = iota
	KindNegInt
	KindBytes
	KindText
	KindArray
	KindMap
	KindTag
	KindSharedRef // tag 29: reference into the shared-value table
	KindSimple
	KindFloat
)

func (k Kind) String() string {
	switch k {
	case KindUint:
		return "uint"
	case KindNegInt:
		return "negint"
	case KindBytes:
		return "bytes"
	case KindText:
		return "text"
	case KindArray:
		return "array"
	case KindMap:
		return "map"
	case KindTag:
		return "tag"
	case KindSharedRef:
		return "sharedref"
	case KindSimple:
		return "simple"
	case KindFloat:
		return "float"
	}
	return "unknown"
}

// Node is one decoded CBOR item. Start/End delimit the exact byte range of
// the item inside the original input; the original bytes are never mutated.
type Node struct {
	Kind    Kind
	Start   int  // offset of the first byte, inclusive
	HeadLen int  // length of the item head (initial byte + argument)
	End     int  // offset past the last byte
	HeadAI  byte // original additional-info bits, for lossless re-encoding
	Indef   bool // item used indefinite-length encoding

	Uint   uint64 // KindUint; also tag number for KindTag
	Int    int64  // KindNegInt (negative value)
	Raw    []byte // KindBytes payload / concatenated chunks
	Text   string // KindText payload / concatenated chunks
	Float  float64
	Bits   uint64 // original float bit pattern at Width
	Width  int    // float width in bytes: 2, 4 or 8
	Simple byte

	TagNum   uint64
	ShareIdx int     // index assigned to a tag-28 shareable value, -1 otherwise
	Children []*Node // array elements, string chunks, tag child (len 1)
	Keys     []*Node
	Vals     []*Node

	RefIdx    uint64 // KindSharedRef: referenced shared-value index
	RefTarget *Node  // resolved tag-28 node, nil when undefined

	Broken bool   // subtree failed to decode cleanly
	Note   string // short decode-time note for broken nodes
	Keep   []byte // original bytes to replay for a recovered broken subtree
}

// Diagnostic describes one finding, tied to a byte range of the input.
type Diagnostic struct {
	Code     string // nan-payload, neg-zero, dup-key, broken-chunk, undefined-ref, cyclic-ref, ...
	Severity string // info, warn, error
	Msg      string
	Start    int
	End      int
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("[%s] %s @%d..%d: %s", d.Severity, d.Code, d.Start, d.End, d.Msg)
}

// Step records one rewrite performed while producing a canonical candidate.
type Step struct {
	Path string
	Msg  string
}

// Range returns the half-open byte range of the node.
func (n *Node) Range() (int, int) { return n.Start, n.End }

// Head returns the head byte range of the node.
func (n *Node) Head() (int, int) { return n.Start, n.Start + n.HeadLen }

// Summary is a short human-readable one-liner for the parse tree.
func (n *Node) Summary() string {
	switch n.Kind {
	case KindUint:
		return fmt.Sprintf("uint %d", n.Uint)
	case KindNegInt:
		return fmt.Sprintf("int %d", n.Int)
	case KindBytes:
		return fmt.Sprintf("bytes(%d)", len(n.Raw))
	case KindText:
		return fmt.Sprintf("text %q", truncate(n.Text, 24))
	case KindArray:
		return fmt.Sprintf("array(%d)", len(n.Children))
	case KindMap:
		return fmt.Sprintf("map(%d)", len(n.Keys))
	case KindTag:
		if n.TagNum == 28 {
			return fmt.Sprintf("tag 28 shareable #%d", n.ShareIdx)
		}
		return fmt.Sprintf("tag %d", n.TagNum)
	case KindSharedRef:
		return fmt.Sprintf("sharedref #%d", n.RefIdx)
	case KindSimple:
		return fmt.Sprintf("simple(%d)", n.Simple)
	case KindFloat:
		return fmt.Sprintf("float%d %v", n.Width*8, n.Float)
	}
	return "?"
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
