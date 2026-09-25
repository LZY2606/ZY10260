package cbor

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
)

// canon carries state for one deterministic-encoding pass.
type canon struct {
	steps    []Step
	diags    []Diagnostic
	visiting map[*Node]bool
	failed   bool
}

func (c *canon) step(path, msg string) {
	c.steps = append(c.steps, Step{Path: path, Msg: msg})
}

func (c *canon) diag(code, sev string, start, end int, msg string) {
	c.diags = append(c.diags, Diagnostic{Code: code, Severity: sev, Msg: msg, Start: start, End: end})
}

// Result is the outcome of one canonicalization pass.
type Result struct {
	Data   []byte       // deterministic encoding candidate
	Digest string       // hex SHA-256 of Data
	Steps  []Step       // every rewrite applied, in order
	Diags  []Diagnostic // diagnostics raised during canonicalization
	OK     bool         // false when any subtree had to be substituted
}

// Canonicalize produces the RFC 8949 deterministic-encoding candidate for a
// decoded tree. The original input and the lenient tree are never modified.
// A broken or cyclic subtree degrades to a null placeholder with a
// diagnostic; confirmed siblings are preserved.
func Canonicalize(roots ...*Node) Result {
	c := &canon{visiting: map[*Node]bool{}}
	var b bytes.Buffer
	for i, n := range roots {
		c.encodeNode(&b, n, "/"+itoa(i))
	}
	ok := !c.failed
	sum := sha256.Sum256(b.Bytes())
	return Result{
		Data:   b.Bytes(),
		Digest: hex.EncodeToString(sum[:]),
		Steps:  c.steps,
		Diags:  c.diags,
		OK:     ok,
	}
}

// DigestBytes returns the hex SHA-256 digest of data.
func DigestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
