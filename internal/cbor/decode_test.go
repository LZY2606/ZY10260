package cbor

import (
	"bytes"
	"encoding/hex"
	"math"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	clean := bytes.Map(func(r rune) rune {
		if r == ' ' || r == '\n' {
			return -1
		}
		return r
	}, []byte(s))
	b, err := hex.DecodeString(string(clean))
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

func hasDiag(diags []Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

// Byte ranges: every node reports the exact half-open range of its encoding.
func TestByteRanges(t *testing.T) {
	data := mustHex(t, "bf 61 61 9f 7f 62 68 69 60 ff 18 2a ff ff")
	n, diags := Decode(data)
	if hasDiag(diags, "truncated") {
		t.Fatalf("unexpected truncation: %v", diags)
	}
	if n.Kind != KindMap || !n.Indef {
		t.Fatalf("root = %v indef=%v", n.Kind, n.Indef)
	}
	if n.Start != 0 || n.End != len(data) {
		t.Fatalf("root range [%d,%d), want [0,%d)", n.Start, n.End, len(data))
	}
	key := n.Keys[0]
	if key.Kind != KindText || key.Text != "a" || key.Start != 1 || key.End != 3 {
		t.Fatalf("key %+v", key)
	}
	arr := n.Vals[0]
	if arr.Kind != KindArray || arr.Start != 3 || arr.End != 13 {
		t.Fatalf("array range [%d,%d)", arr.Start, arr.End)
	}
	str := arr.Children[0]
	if str.Kind != KindText || !str.Indef || str.Text != "hi" {
		t.Fatalf("indef text %+v", str)
	}
	if len(str.Children) != 2 || str.Children[0].Start != 5 || str.Children[0].End != 8 {
		t.Fatalf("chunks %+v", str.Children)
	}
	u := arr.Children[1]
	if u.Kind != KindUint || u.Uint != 42 || u.Start != 10 || u.End != 12 || u.HeadLen != 2 {
		t.Fatalf("uint %+v", u)
	}
}

// 0x18 0x01 is a non-shortest encoding of 1 and must be noted.
func TestNonShortest(t *testing.T) {
	_, diags := Decode(mustHex(t, "18 01"))
	if !hasDiag(diags, "non-shortest") {
		t.Fatalf("want non-shortest diagnostic, got %v", diags)
	}
}

// Lenient re-encoding must reproduce the original bytes exactly.
func TestLenientRoundTrip(t *testing.T) {
	cases := []string{
		"bf 61 61 9f 7f 62 68 69 60 ff 18 2a ff ff",
		"a2 01 61 78 18 01 61 79",
		"82 d8 1c 61 61 d8 1d 00",
		"9f fb 3ff8000000000000 fb 3fb999999999999a fb 47efffffe0000000 fb 8000000000000000 fb 7ff8000000000001 ff",
		"83 7f 41 00 ff 01 02",
		"d8 1d 07",
		"fa 47c35000 f9 3c00 f8 ff e0",
	}
	for _, h := range cases {
		data := mustHex(t, h)
		roots, _ := DecodeStream(data)
		var out []byte
		for _, r := range roots {
			out = append(out, EncodeLenient(r)...)
		}
		if !bytes.Equal(out, data) {
			t.Fatalf("round trip mismatch for %s:\n got %x\nwant %x", h, out, data)
		}
	}
}

// Shared references must resolve to the identical node, not a copy.
func TestReferenceIdentity(t *testing.T) {
	data := mustHex(t, "82 d8 1c 61 61 d8 1d 00")
	n, diags := Decode(data)
	if hasDiag(diags, "undefined-ref") {
		t.Fatalf("unexpected undefined-ref: %v", diags)
	}
	if n.Kind != KindArray || len(n.Children) != 2 {
		t.Fatalf("root %+v", n)
	}
	shareable, ref := n.Children[0], n.Children[1]
	if shareable.Kind != KindTag || shareable.TagNum != 28 || shareable.ShareIdx != 0 {
		t.Fatalf("shareable %+v", shareable)
	}
	if ref.Kind != KindSharedRef || ref.RefIdx != 0 {
		t.Fatalf("ref %+v", ref)
	}
	if ref.RefTarget != shareable {
		t.Fatalf("RefTarget %p, want identical node %p", ref.RefTarget, shareable)
	}
}

// A shareable may reference itself; identity must still hold and the
// canonicalizer must diagnose the cycle instead of looping.
func TestSharedCycle(t *testing.T) {
	data := mustHex(t, "d8 1c 81 d8 1d 00")
	n, _ := Decode(data)
	if n.Kind != KindTag || n.TagNum != 28 {
		t.Fatalf("root %+v", n)
	}
	arr := n.Children[0]
	ref := arr.Children[0]
	if ref.RefTarget != n {
		t.Fatalf("cycle RefTarget %p, want root %p", ref.RefTarget, n)
	}
	res := Canonicalize(n)
	if res.OK {
		t.Fatalf("cyclic input must not canonicalize cleanly")
	}
	if !hasDiag(res.Diags, "cyclic-ref") {
		t.Fatalf("want cyclic-ref diagnostic, got %v", res.Diags)
	}
	// second pass over the degraded output is stable
	roots, _ := DecodeStream(res.Data)
	re := Canonicalize(roots...)
	if !bytes.Equal(re.Data, res.Data) {
		t.Fatalf("degraded canonical form not idempotent: %x vs %x", re.Data, res.Data)
	}
}

// A broken chunk must not destroy confirmed siblings.
func TestBrokenChunkRecovery(t *testing.T) {
	data := mustHex(t, "83 7f 41 00 ff 01 02")
	n, diags := Decode(data)
	if !hasDiag(diags, "broken-chunk") {
		t.Fatalf("want broken-chunk diagnostic, got %v", diags)
	}
	if n.Kind != KindArray || len(n.Children) != 3 {
		t.Fatalf("array should keep 3 children, got %+v", n.Children)
	}
	if !n.Children[0].Broken {
		t.Fatalf("first child should be broken")
	}
	if n.Children[1].Kind != KindUint || n.Children[1].Uint != 1 {
		t.Fatalf("sibling 1 lost: %+v", n.Children[1])
	}
	if n.Children[2].Kind != KindUint || n.Children[2].Uint != 2 {
		t.Fatalf("sibling 2 lost: %+v", n.Children[2])
	}
	res := Canonicalize(n)
	if hasDiag(res.Diags, "broken-subtree") == false {
		t.Fatalf("want broken-subtree diagnostic, got %v", res.Diags)
	}
	// siblings survive inside the canonical candidate: 83 f6 01 02
	want := mustHex(t, "83 f6 01 02")
	if !bytes.Equal(res.Data, want) {
		t.Fatalf("canonical = %x, want %x", res.Data, want)
	}
}

func TestTruncatedTag(t *testing.T) {
	data := mustHex(t, "d8 2a")
	n, diags := Decode(data)
	if !hasDiag(diags, "truncated") {
		t.Fatalf("want truncated diagnostic, got %v", diags)
	}
	if n.Kind != KindTag || n.TagNum != 42 {
		t.Fatalf("tag number must be preserved, got %+v", n)
	}
}

func TestBudgetAndBounds(t *testing.T) {
	// array(2^64-1) must be rejected before any allocation
	n, diags := Decode(mustHex(t, "9b ffffffffffffffff"))
	if !hasDiag(diags, "budget-exceeded") {
		t.Fatalf("want budget-exceeded, got %v", diags)
	}
	if !n.Broken {
		t.Fatalf("node should be broken")
	}
	// string declaring more bytes than remain
	_, diags = Decode(mustHex(t, "5a 00010000 41 00"))
	if !hasDiag(diags, "truncated") {
		t.Fatalf("want truncated, got %v", diags)
	}
	// truncated 8-byte argument
	_, diags = Decode(mustHex(t, "db 00 00"))
	if !hasDiag(diags, "truncated") {
		t.Fatalf("want truncated for tag argument, got %v", diags)
	}
}

func TestFloatDiagnostics(t *testing.T) {
	_, diags := Decode(mustHex(t, "fb 7ff8000000000001"))
	if !hasDiag(diags, "nan-payload") {
		t.Fatalf("want nan-payload, got %v", diags)
	}
	_, diags = Decode(mustHex(t, "fb 8000000000000000"))
	if !hasDiag(diags, "neg-zero") {
		t.Fatalf("want neg-zero, got %v", diags)
	}
	// canonical NaN must not be flagged
	_, diags = Decode(mustHex(t, "f9 7e00"))
	if hasDiag(diags, "nan-payload") {
		t.Fatalf("canonical NaN flagged: %v", diags)
	}
}

func TestUndefinedRef(t *testing.T) {
	_, diags := Decode(mustHex(t, "d8 1d 07"))
	if !hasDiag(diags, "undefined-ref") {
		t.Fatalf("want undefined-ref, got %v", diags)
	}
}

func TestHalfFloatConversion(t *testing.T) {
	cases := []struct {
		bits uint16
		want float64
	}{
		{0x3c00, 1.0},
		{0x3e00, 1.5},
		{0x0001, 5.960464477539063e-8},
		{0x7bff, 65504},
		{0x8000, -0.0},
	}
	for _, c := range cases {
		if got := halfToFloat(c.bits); got != c.want {
			t.Fatalf("halfToFloat(%04x) = %v, want %v", c.bits, got, c.want)
		}
	}
	if h, ok := exactHalf(1.5); !ok || h != 0x3e00 {
		t.Fatalf("exactHalf(1.5) = %04x,%v", h, ok)
	}
	if _, ok := exactHalf(0.1); ok {
		t.Fatalf("0.1 must not be half-exact")
	}
	negZero := math.Copysign(0, -1)
	if h, ok := exactHalf(negZero); !ok || h != 0x8000 {
		t.Fatalf("exactHalf(-0.0) = %04x,%v", h, ok)
	}
}
