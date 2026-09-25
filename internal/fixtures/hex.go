package fixtures

import "encoding/hex"

func mustDecodeHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("bad fixture hex: " + err.Error())
	}
	return b
}
