package cbor

import (
	"bytes"
	"encoding/binary"
	"math"
	"sort"
)

// EncodeLenient re-encodes a parse tree preserving every original encoding
// choice (argument widths, indefinite markers, float widths). For a cleanly
// decoded input the output is byte-identical to the original.
func EncodeLenient(n *Node) []byte {
	var b bytes.Buffer
	encodeLenient(&b, n)
	return b.Bytes()
}

func writeHeadAI(b *bytes.Buffer, major, ai byte, arg uint64) {
	b.WriteByte(major<<5 | ai)
	switch ai {
	case 24:
		b.WriteByte(byte(arg))
	case 25:
		var t [2]byte
		binary.BigEndian.PutUint16(t[:], uint16(arg))
		b.Write(t[:])
	case 26:
		var t [4]byte
		binary.BigEndian.PutUint32(t[:], uint32(arg))
		b.Write(t[:])
	case 27:
		var t [8]byte
		binary.BigEndian.PutUint64(t[:], arg)
		b.Write(t[:])
	}
}

func encodeLenient(b *bytes.Buffer, n *Node) {
	if len(n.Keep) > 0 {
		b.Write(n.Keep)
		return
	}
	switch n.Kind {
	case KindUint:
		writeHeadAI(b, 0, n.HeadAI, n.Uint)
	case KindNegInt:
		writeHeadAI(b, 1, n.HeadAI, uint64(-1-n.Int))
	case KindBytes, KindText:
		major := byte(2)
		if n.Kind == KindText {
			major = 3
		}
		if n.Indef {
			b.WriteByte(major<<5 | 31)
			for _, c := range n.Children {
				encodeLenient(b, c)
			}
			b.WriteByte(0xff)
			return
		}
		writeHeadAI(b, major, n.HeadAI, uint64(len(n.Raw)))
		b.Write(n.Raw)
	case KindArray:
		if n.Indef {
			b.WriteByte(0x9f)
			for _, c := range n.Children {
				encodeLenient(b, c)
			}
			b.WriteByte(0xff)
			return
		}
		writeHeadAI(b, 4, n.HeadAI, uint64(len(n.Children)))
		for _, c := range n.Children {
			encodeLenient(b, c)
		}
	case KindMap:
		if n.Indef {
			b.WriteByte(0xbf)
			for i := range n.Keys {
				encodeLenient(b, n.Keys[i])
				encodeLenient(b, n.Vals[i])
			}
			b.WriteByte(0xff)
			return
		}
		writeHeadAI(b, 5, n.HeadAI, uint64(len(n.Keys)))
		for i := range n.Keys {
			encodeLenient(b, n.Keys[i])
			encodeLenient(b, n.Vals[i])
		}
	case KindTag:
		writeHeadAI(b, 6, n.HeadAI, n.TagNum)
		if len(n.Children) == 1 {
			encodeLenient(b, n.Children[0])
		}
	case KindSharedRef:
		writeHeadAI(b, 6, n.HeadAI, 29)
		if len(n.Children) == 1 {
			encodeLenient(b, n.Children[0])
		}
	case KindSimple:
		if n.HeadAI == 24 {
			b.WriteByte(0xf8)
			b.WriteByte(n.Simple)
		} else {
			b.WriteByte(0xe0 | n.Simple)
		}
	case KindFloat:
		switch n.Width {
		case 2:
			b.WriteByte(0xf9)
			var t [2]byte
			binary.BigEndian.PutUint16(t[:], uint16(n.Bits))
			b.Write(t[:])
		case 4:
			b.WriteByte(0xfa)
			var t [4]byte
			binary.BigEndian.PutUint32(t[:], uint32(n.Bits))
			b.Write(t[:])
		default:
			b.WriteByte(0xfb)
			var t [8]byte
			binary.BigEndian.PutUint64(t[:], n.Bits)
			b.Write(t[:])
		}
	}
}

// writeShortestHead writes a head using the shortest argument form.
func writeShortestHead(b *bytes.Buffer, major byte, arg uint64) {
	switch {
	case arg < 24:
		b.WriteByte(major<<5 | byte(arg))
	case arg < 1<<8:
		b.WriteByte(major<<5 | 24)
		b.WriteByte(byte(arg))
	case arg < 1<<16:
		b.WriteByte(major<<5 | 25)
		var t [2]byte
		binary.BigEndian.PutUint16(t[:], uint16(arg))
		b.Write(t[:])
	case arg < 1<<32:
		b.WriteByte(major<<5 | 26)
		var t [4]byte
		binary.BigEndian.PutUint32(t[:], uint32(arg))
		b.Write(t[:])
	default:
		b.WriteByte(major<<5 | 27)
		var t [8]byte
		binary.BigEndian.PutUint64(t[:], arg)
		b.Write(t[:])
	}
}

// encodeDeterministic appends the RFC 8949 deterministic encoding of n.
// visiting tracks in-progress shareable nodes for cycle detection.
// It returns false when the subtree could not be encoded (caller substitutes
// a placeholder and keeps going, so siblings are never lost).
func (e *canon) encodeNode(b *bytes.Buffer, n *Node, path string) bool {
	if n.Broken {
		e.diag("broken-subtree", "error", n.Start, n.End,
			"broken subtree replaced by null in canonical candidate: "+n.Note)
		b.WriteByte(0xf6)
		e.failed = true
		return false
	}
	switch n.Kind {
	case KindUint:
		writeShortestHead(b, 0, n.Uint)
	case KindNegInt:
		writeShortestHead(b, 1, uint64(-1-n.Int))
	case KindBytes, KindText:
		major := byte(2)
		if n.Kind == KindText {
			major = 3
		}
		if n.Indef {
			e.step(path, "indefinite-length string converted to definite length")
		}
		writeShortestHead(b, major, uint64(len(n.Raw)))
		b.Write(n.Raw)
	case KindArray:
		if n.Indef {
			e.step(path, "indefinite-length array converted to definite length")
		}
		writeShortestHead(b, 4, uint64(len(n.Children)))
		for i, c := range n.Children {
			e.encodeNode(b, c, path+"/"+itoa(i))
		}
	case KindMap:
		e.encodeMap(b, n, path)
	case KindTag:
		if n.TagNum == 28 {
			// A shareable value encodes as tag 28 around its content.
			// Cycle guards live on the sharedref side.
			writeShortestHead(b, 6, 28)
			ok := true
			if len(n.Children) == 1 {
				ok = e.encodeNode(b, n.Children[0], path+"/shareable")
			}
			return ok
		}
		writeShortestHead(b, 6, n.TagNum)
		if len(n.Children) == 1 {
			e.encodeNode(b, n.Children[0], path+"/tagged")
		}
	case KindSharedRef:
		if n.RefTarget == nil {
			e.diag("undefined-ref", "error", n.Start, n.End,
				"undefined shared reference kept as tag 29 placeholder")
			writeShortestHead(b, 6, 29)
			writeShortestHead(b, 0, n.RefIdx)
			e.failed = true
			return false
		}
		if e.visiting[n.RefTarget] {
			e.diag("cyclic-ref", "error", n.Start, n.End,
				"shared reference forms a cycle; replaced by null")
			b.WriteByte(0xf6)
			e.failed = true
			return false
		}
		e.step(path, "shared reference expanded inline to shareable content")
		e.visiting[n.RefTarget] = true
		ok := true
		if len(n.RefTarget.Children) == 1 {
			ok = e.encodeNode(b, n.RefTarget.Children[0], path+"/ref")
		}
		delete(e.visiting, n.RefTarget)
		return ok
	case KindSimple:
		if n.Simple < 24 {
			b.WriteByte(0xe0 | n.Simple)
		} else {
			b.WriteByte(0xf8)
			b.WriteByte(n.Simple)
		}
	case KindFloat:
		e.encodeFloat(b, n, path)
	}
	return true
}

func (e *canon) encodeMap(b *bytes.Buffer, n *Node, path string) {
	if n.Indef {
		e.step(path, "indefinite-length map converted to definite length")
	}
	type kv struct {
		kb   []byte
		k, v *Node
		i    int
	}
	pairs := make([]kv, 0, len(n.Keys))
	seen := map[string]int{}
	for i := range n.Keys {
		var kb bytes.Buffer
		e.encodeNode(&kb, n.Keys[i], path+"/key")
		key := kb.Bytes()
		if prev, dup := seen[string(key)]; dup {
			e.diag("dup-key", "error", n.Keys[i].Start, n.Keys[i].End,
				"duplicate map key: deterministic encoding equal to entry "+itoa(prev))
		} else {
			seen[string(key)] = i
		}
		pairs = append(pairs, kv{kb: key, k: n.Keys[i], v: n.Vals[i], i: i})
	}
	sort.SliceStable(pairs, func(a, c int) bool {
		if len(pairs[a].kb) != len(pairs[c].kb) {
			return len(pairs[a].kb) < len(pairs[c].kb)
		}
		return bytes.Compare(pairs[a].kb, pairs[c].kb) < 0
	})
	sorted := true
	for i := range pairs {
		if pairs[i].i != i {
			sorted = false
			break
		}
	}
	if !sorted {
		e.step(path, "map keys reordered by deterministic encoding (length, then bytes)")
	}
	writeShortestHead(b, 5, uint64(len(pairs)))
	for _, p := range pairs {
		b.Write(p.kb)
		e.encodeNode(b, p.v, path+"/val")
	}
}

func (e *canon) encodeFloat(b *bytes.Buffer, n *Node, path string) {
	f := n.Float
	if math.IsNaN(f) {
		if n.Bits != 0x7e00 || n.Width != 2 {
			e.step(path, "NaN payload collapsed to canonical half 0xf97e00")
		}
		b.WriteByte(0xf9)
		b.Write([]byte{0x7e, 0x00})
		return
	}
	if h, ok := exactHalf(f); ok {
		if n.Width != 2 {
			e.step(path, "float narrowed to half precision without value change")
		}
		b.WriteByte(0xf9)
		var t [2]byte
		binary.BigEndian.PutUint16(t[:], h)
		b.Write(t[:])
		return
	}
	if s, ok := exactSingle(f); ok {
		if n.Width != 4 {
			e.step(path, "float narrowed to single precision without value change")
		}
		b.WriteByte(0xfa)
		var t [4]byte
		binary.BigEndian.PutUint32(t[:], s)
		b.Write(t[:])
		return
	}
	b.WriteByte(0xfb)
	var t [8]byte
	binary.BigEndian.PutUint64(t[:], math.Float64bits(f))
	b.Write(t[:])
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [8]byte
	p := len(buf)
	for i > 0 {
		p--
		buf[p] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[p:])
}
