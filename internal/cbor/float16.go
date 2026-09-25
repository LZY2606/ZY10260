package cbor

import "math"

// halfToFloat converts an IEEE 754 binary16 bit pattern to float64.
func halfToFloat(h uint16) float64 {
	sign := uint64(h>>15) & 1
	exp := (h >> 10) & 0x1f
	frac := uint64(h) & 0x3ff
	switch {
	case exp == 0:
		if frac == 0 {
			return math.Copysign(0, float64(1-2*int64(sign)))
		}
		f := math.Ldexp(float64(frac), -24)
		if sign == 1 {
			return -f
		}
		return f
	case exp == 0x1f:
		if frac == 0 {
			return math.Inf(1 - 2*int(sign))
		}
		return math.NaN()
	default:
		f := math.Ldexp(float64(frac|0x400), int(exp)-25)
		if sign == 1 {
			return -f
		}
		return f
	}
}

// floatToHalf converts float64 to binary16 with round-to-nearest-even.
func floatToHalf(f float64) uint16 {
	bits := math.Float64bits(f)
	sign := uint16(bits>>63) << 15
	exp := int(bits>>52) & 0x7ff
	frac := bits & 0xfffffffffffff
	if exp == 0x7ff { // Inf / NaN
		if frac == 0 {
			return sign | 0x7c00
		}
		return sign | 0x7e00
	}
	e := exp - 1023 + 15
	switch {
	case e >= 0x1f: // overflow -> Inf
		return sign | 0x7c00
	case e <= 0: // subnormal or zero (e = unbiased exponent + 15)
		if e < -10 {
			return sign
		}
		m := frac | 0x10000000000000
		shift := uint(43 - e)
		half := uint16(m >> shift)
		rem := m & ((1 << shift) - 1)
		halfway := uint64(1) << (shift - 1)
		if rem > halfway || (rem == halfway && half&1 == 1) {
			half++
		}
		return sign | half
	default:
		half := uint16(e)<<10 | uint16(frac>>42)
		rem := frac & 0x3ffffffffff
		const halfway = 0x20000000000
		if rem > halfway || (rem == halfway && half&1 == 1) {
			half++
		}
		return sign | half
	}
}

// exactHalf returns the binary16 bits of f when f is exactly representable.
func exactHalf(f float64) (uint16, bool) {
	h := floatToHalf(f)
	back := halfToFloat(h)
	if math.IsNaN(f) {
		return h, math.IsNaN(back)
	}
	return h, back == f && math.Signbit(back) == math.Signbit(f)
}

// exactSingle reports whether f is exactly representable as binary32.
func exactSingle(f float64) (uint32, bool) {
	s := float32(f)
	back := float64(s)
	if math.IsNaN(f) {
		return math.Float32bits(s), true
	}
	return math.Float32bits(s), back == f && math.Signbit(back) == math.Signbit(f)
}
