// Package fixtures holds the built-in corpus: every entry is a concrete
// byte string that exercises one diagnostic or rewrite path.
package fixtures

import "strings"

type Fixture struct {
	Name string
	Desc string
	Hex  string
}

var All = []Fixture{
	{
		Name: "nested-indefinite",
		Desc: "三层 indefinite 容器：indefinite map 内含 indefinite array，再含分段 indefinite text",
		Hex:  "bf 61 61 9f 7f 62 68 69 60 ff 18 2a ff ff",
	},
	{
		Name: "equiv-keys",
		Desc: "同一语义 key 1 的两种编码：0x01 与非最短 0x1801，触发重复 key 与非最短诊断",
		Hex:  "a2 01 61 78 18 01 61 79",
	},
	{
		Name: "shared-identity",
		Desc: "tag 28 可共享值 \"a\"，随后 tag 29 #0 引用；两处引用指向同一节点身份",
		Hex:  "82 d8 1c 61 61 d8 1d 00",
	},
	{
		Name: "shared-cycle",
		Desc: "共享环：tag 28 包裹数组，数组元素 tag 29 #0 指回自身",
		Hex:  "d8 1c 81 d8 1d 00",
	},
	{
		Name: "float-narrowing",
		Desc: "浮点收窄边界：1.5→half，0.1 保持 double，float32 上限→single，-0.0，带 payload 的 NaN",
		Hex: "9f fb 3ff8000000000000 fb 3fb999999999999a " +
			"fb 47efffffe0000000 fb 8000000000000000 fb 7ff8000000000001 ff",
	},
	{
		Name: "truncated-tag",
		Desc: "截断 tag：tag 42 之后输入直接结束，子值缺失",
		Hex:  "d8 2a",
	},
	{
		Name: "broken-chunk",
		Desc: "断裂 chunk：indefinite text 中混入 byte-string 块，后续相邻项仍可恢复",
		Hex:  "83 7f 41 00 ff 01 02",
	},
	{
		Name: "undefined-ref",
		Desc: "未定义共享引用：tag 29 #7 没有对应的 tag 28",
		Hex:  "d8 1d 07",
	},
}

// Bytes returns the fixture payload with spaces / 0x prefixes stripped.
func (f Fixture) Bytes() []byte {
	s := strings.ReplaceAll(f.Hex, " ", "")
	s = strings.ReplaceAll(s, "0x", "")
	return mustDecodeHex(s)
}
