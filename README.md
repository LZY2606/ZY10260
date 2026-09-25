# 定序封签台（CBOR 离线取证工作台）

面向取证人员的离线工作台：导入来自不同设备的 CBOR 项，**逐字节保留原输入**，
并排展示三栏：

1. **原字节** — 导入后只写一次（SQLite 中 `INSERT OR IGNORE`），永不覆盖；
2. **宽松解析** — 容错解析树（精确字节区间、引用身份、诊断）+ 无损重编码，
   干净输入的重编码与原字节逐字节一致（digest 相同）；
3. **规范化候选** — RFC 8949 deterministic profile，逐步改写说明 + SHA-256 摘要，
   并在页面上即时验证第二次规范化是否幂等。

不依赖账号、云服务或随机网络时序；唯一外部依赖是纯 Go 的 `modernc.org/sqlite`（无 cgo）。

## 安装

```sh
go mod download && go build ./...
```

## 验证与演示

```sh
go test ./... -count=1
go run ./cmd/server --addr 127.0.0.1:5960
# 浏览器访问 http://127.0.0.1:5960，页面标题为「定序封签台」
```

可选参数：`--db path/to/work.db`（默认 `cborbench.db`）。首次启动若库为空，
自动播种 8 条内置语料。

## 解码器能力

- definite 与 indefinite 的 byte/text string、array、map（含嵌套 indefinite）；
- simple value（含 0..19 直编码与 24..255 一字节形式、reserved 诊断）；
- half(16) / single(32) / double(64) 浮点，含二进制16精确收窄；
- tag：**未知 tag 一律保留编号与子值，不擅自解释**（仅记 `unknown-tag` 信息）；
- tag 28（shareable）/ tag 29（sharedref）共享值引用，按节点身份解析；
- 所有声明长度先做边界与预算检查（嵌套深度 64、总项数 2^20、字符串 16 MiB）。

## 诊断项

| 代码 | 含义 |
| --- | --- |
| `nan-payload` | NaN 携带非规范 payload（或符号位） |
| `neg-zero` | 负零 `-0.0` |
| `dup-key` | 规范化编码后重复的 map key |
| `broken-chunk` | indefinite string 中出现错误类型 chunk |
| `undefined-ref` | tag 29 指向不存在的共享值 |
| `cyclic-ref` | 共享引用成环 |
| `non-shortest` / `reserved-simple` / `reserved-ai` | 非最短整数、保留 simple/ai |
| `truncated` / `budget-exceeded` / `depth-exceeded` | 截断、长度预算、嵌套深度 |

坏子树不拖垮邻居：断裂 chunk 会重同步到 break，缺脑子树在规范化候选中降级为
`null`（`broken-subtree` 诊断），**相邻已确认项原样保留**。

## 规范化规则（RFC 8949 deterministic）

- 整数最短形式；indefinite 容器一律转 definite；
- 浮点优先精确收窄 half → single → double；NaN 统一为 `f9 7e00`；`-0.0` → `f9 8000`；
- **map key 按确定性编码后的「长度，再字节序」排序，不按显示文本排序**；
- 共享引用内联为共享值内容；未知 tag 编号原样保留。

## 内置语料（首次启动自动入库）

`nested-indefinite`（三层 indefinite 容器）、`equiv-keys`（同一 key 的两种编码）、
`shared-identity`（引用身份）、`shared-cycle`（共享环）、`float-narrowing`
（half/single/double 收窄边界、-0.0、NaN payload）、`truncated-tag`（截断 tag）、
`broken-chunk`（断裂 chunk + 相邻项恢复）、`undefined-ref`（未定义引用）。

## 测试覆盖

- 字节区间断言（`[start,end)` 与 head 长度）；
- 宽松重编码与原始多组用例逐字节相等（无损往返）；
- 共享引用的指针身份与成环检测；
- 第二次规范化幂等（字节与 digest 固定点）；
- 坏子树不丢失相邻已确认项；
- SQLite 原始版本永不覆盖、重开持久化；
- HTTP 页面标题、三栏、SVG、导入失败提示。

## 目录结构

```
cmd/server/         HTTP 入口
internal/cbor/      解码器、无损重编码、确定性编码（含 float16）
internal/fixtures/  内置语料
internal/store/     SQLite 与迁移（PRAGMA user_version）
internal/web/       页面模板、hex dump、SVG 字节范围图
```
