package cbor

import (
	"bytes"
	"testing"
)

func canonOf(t *testing.T, hexIn string) Result {
	t.Helper()
	roots, _ := DecodeStream(mustHex(t, hexIn))
	return Canonicalize(roots...)
}

// Second canonicalization over the candidate must be a fixed point.
func TestIdempotent(t *testing.T) {
	cases := []string{
		"bf 61 61 9f 7f 62 68 69 60 ff 18 2a ff ff",
		"a2 01 61 78 18 01 61 79",
		"82 d8 1c 61 61 d8 1d 00",
		"9f fb 3ff8000000000000 fb 3fb999999999999a fb 47efffffe0000000 fb 8000000000000000 fb 7ff8000000000001 ff",
		"83 7f 41 00 ff 01 02",
		"d8 1d 07",
		"a4 0a 01 01 02 61 61 03 19 03e8 04",
	}
	for _, h := range cases {
		first := canonOf(t, h)
		roots, _ := DecodeStream(first.Data)
		second := Canonicalize(roots...)
		if !bytes.Equal(first.Data, second.Data) {
			t.Fatalf("%s: not idempotent\nfirst  %x\nsecond %x", h, first.Data, second.Data)
		}
		if first.Digest != second.Digest {
			t.Fatalf("%s: digest drift %s vs %s", h, first.Digest, second.Digest)
		}
	}
}

// Map keys sort by deterministic encoding: length first, then bytes —
// never by display text.
func TestMapKeyOrdering(t *testing.T) {
	// keys in wire order: 1000, "a", 10, 1 — canonical order must be
	// 01, 0a, 6161, 1903e8 (by encoded length then bytes)
	res := canonOf(t, "a4 19 03e8 01 61 61 02 0a 03 01 04")
	want := mustHex(t, "a4 01 04 0a 03 61 61 02 19 03e8 01")
	if !bytes.Equal(res.Data, want) {
		t.Fatalf("got %x want %x", res.Data, want)
	}
	found := false
	for _, s := range res.Steps {
		if s.Msg == "map keys reordered by deterministic encoding (length, then bytes)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing reorder step: %+v", res.Steps)
	}
}

// Two encodings of the same semantic key collapse to a duplicate diagnostic.
func TestDuplicateKeys(t *testing.T) {
	res := canonOf(t, "a2 01 61 78 18 01 61 79")
	if !hasDiag(res.Diags, "dup-key") {
		t.Fatalf("want dup-key, got %v", res.Diags)
	}
	// canonical output still carries both pairs deterministically
	want := mustHex(t, "a2 01 61 78 01 61 79")
	if !bytes.Equal(res.Data, want) {
		t.Fatalf("got %x want %x", res.Data, want)
	}
}

func TestFloatNarrowing(t *testing.T) {
	res := canonOf(t, "9f fb 3ff8000000000000 fb 3fb999999999999a fb 47efffffe0000000 fb 8000000000000000 fb 7ff8000000000001 ff")
	want := mustHex(t, "85 f9 3e00 fb 3fb999999999999a fa 7f7fffff f9 8000 f9 7e00")
	if !bytes.Equal(res.Data, want) {
		t.Fatalf("got %x want %x", res.Data, want)
	}
	var narrow, nan int
	for _, s := range res.Steps {
		switch s.Msg {
		case "float narrowed to half precision without value change":
			narrow++
		case "float narrowed to single precision without value change":
			narrow++
		case "NaN payload collapsed to canonical half 0xf97e00":
			nan++
		}
	}
	if narrow != 3 || nan != 1 {
		t.Fatalf("steps narrow=%d nan=%d: %+v", narrow, nan, res.Steps)
	}
}

// Unknown tags keep their number and child without interpretation.
func TestUnknownTagPreserved(t *testing.T) {
	res := canonOf(t, "d8 63 61 61")
	want := mustHex(t, "d8 63 61 61")
	if !bytes.Equal(res.Data, want) {
		t.Fatalf("got %x want %x", res.Data, want)
	}
	_, diags := Decode(mustHex(t, "d8 63 61 61"))
	if !hasDiag(diags, "unknown-tag") {
		t.Fatalf("want unknown-tag note, got %v", diags)
	}
}

// A defined shared reference expands to its shareable value exactly once.
func TestSharedExpansion(t *testing.T) {
	res := canonOf(t, "82 d8 1c 61 61 d8 1d 00")
	want := mustHex(t, "82 d8 1c 61 61 61 61")
	if !bytes.Equal(res.Data, want) {
		t.Fatalf("got %x want %x", res.Data, want)
	}
	if !res.OK {
		t.Fatalf("expansion should succeed: %v", res.Diags)
	}
}

// Undefined references stay as tag 29 placeholders and flag the result.
func TestUndefinedRefCanonical(t *testing.T) {
	res := canonOf(t, "d8 1d 07")
	if res.OK {
		t.Fatalf("undefined ref must flag the result")
	}
	want := mustHex(t, "d8 1d 07")
	if !bytes.Equal(res.Data, want) {
		t.Fatalf("got %x want %x", res.Data, want)
	}
}

// Indefinite containers become definite; steps narrate every rewrite.
func TestIndefiniteToDefinite(t *testing.T) {
	res := canonOf(t, "bf 61 61 9f 7f 62 68 69 60 ff 18 2a ff ff")
	want := mustHex(t, "a1 61 61 82 62 68 69 18 2a")
	if !bytes.Equal(res.Data, want) {
		t.Fatalf("got %x want %x", res.Data, want)
	}
	if len(res.Steps) < 3 {
		t.Fatalf("want >=3 rewrite steps, got %+v", res.Steps)
	}
}

// The original tree and bytes are untouched by canonicalization.
func TestOriginalUntouched(t *testing.T) {
	data := mustHex(t, "bf 61 61 9f 7f 62 68 69 60 ff 18 2a ff ff")
	before := append([]byte(nil), data...)
	roots, _ := DecodeStream(data)
	Canonicalize(roots...)
	if !bytes.Equal(data, before) {
		t.Fatalf("input mutated")
	}
	var out []byte
	for _, r := range roots {
		out = append(out, EncodeLenient(r)...)
	}
	if !bytes.Equal(out, data) {
		t.Fatalf("tree mutated: %x", out)
	}
}
