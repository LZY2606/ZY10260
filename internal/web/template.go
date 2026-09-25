package web

const pageTmpl = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>定序封签台</title>
<style>
:root { color-scheme: light; }
body { font: 14px/1.5 -apple-system, "PingFang SC", sans-serif; margin: 0; background:#f4f6fa; color:#1d2330; }
header { background:#1f2a44; color:#fff; padding:14px 22px; }
header h1 { margin:0; font-size:20px; letter-spacing:4px; }
header p { margin:4px 0 0; font-size:12px; color:#b9c4dd; }
main { padding:18px 22px; }
.card { background:#fff; border:1px solid #d9dee9; border-radius:8px; padding:14px 16px; margin-bottom:16px; }
h2 { font-size:15px; margin:0 0 10px; }
table { border-collapse:collapse; width:100%; font-size:13px; }
td, th { border-bottom:1px solid #e7eaf1; padding:5px 8px; text-align:left; }
th { background:#f0f3f9; }
.cols { display:grid; grid-template-columns: 1fr 1fr 1fr; gap:14px; align-items:start; }
.col h3 { font-size:13px; margin:0 0 8px; padding-bottom:6px; border-bottom:2px solid #1f2a44; }
pre.hex { font:12px/1.7 ui-monospace, monospace; background:#fbfcfe; border:1px solid #e7eaf1; border-radius:6px; padding:8px; overflow:auto; margin:0; }
.digest { font:11px ui-monospace, monospace; word-break:break-all; color:#5a6party; color:#566; }
.tree { list-style:none; padding-left:16px; margin:4px 0; }
.tree > li { margin:2px 0; }
.range { font:11px ui-monospace,monospace; background:#eef2f8; border-radius:4px; padding:0 4px; color:#41506e; }
.kind { font-size:10px; background:#1f2a44; color:#fff; border-radius:3px; padding:0 5px; }
.node.broken > code { color:#b00020; }
.diag { font-size:10px; border-radius:3px; padding:0 5px; cursor:help; }
.diag.error { background:#fde2e4; color:#b00020; }
.diag.warn { background:#fff3cd; color:#8a6d00; }
.diag.info { background:#e0e9f8; color:#23508a; }
em { color:#8a6d00; font-style:normal; font-size:11px; }
.ok { color:#1a7f37; font-weight:600; }
.bad { color:#b00020; font-weight:600; }
input[type=text], textarea { width:100%; box-sizing:border-box; font:13px ui-monospace,monospace; padding:7px; border:1px solid #c6cdda; border-radius:6px; }
button { background:#1f2a44; color:#fff; border:0; border-radius:6px; padding:8px 18px; cursor:pointer; }
small { color:#667; }
.step { font-size:12px; margin:3px 0; padding-left:6px; border-left:3px solid #9ec5fe; }
.step code { font:11px ui-monospace,monospace; color:#41506e; }
.switch a { font-size:12px; margin-right:10px; }
</style>
</head>
<body>
<header>
  <h1>定序封签台</h1>
  <p>离线 CBOR 取证工作台 · 原字节逐字节保留 · RFC 8949 deterministic profile</p>
</header>
<main>
{{if eq .View "index"}}
<div class="card">
  <h2>导入 CBOR 项（hex，允许空格 / 0x 前缀）</h2>
  <form method="post" action="/import">
    <p><input type="text" name="name" placeholder="名称，例如 evidence-01"></p>
    <p><textarea name="hex" rows="3" placeholder="bf 61 61 ..."></textarea></p>
    <button type="submit">导入并排展示</button>
  </form>
</div>
<div class="card">
  <h2>已确认项目（{{len .Items}}）</h2>
  <table>
    <tr><th>ID</th><th>名称</th><th>来源 hex</th><th></th></tr>
    {{range .Items}}
    <tr><td>{{.ID}}</td><td>{{.Name}}</td><td><small>{{.Source}}</small></td>
    <td><a href="/item?id={{.ID}}">查看三栏</a></td></tr>
    {{end}}
  </table>
</div>
<div class="card">
  <h2>内置语料</h2>
  <table>
    <tr><th>名称</th><th>说明</th><th>hex</th></tr>
    {{range .Fixtures}}
    <tr><td>{{.Name}}</td><td>{{.Desc}}</td><td><small>{{.Hex}}</small></td></tr>
    {{end}}
  </table>
  <p><small>首次启动自动播种到 SQLite；语料覆盖嵌套 indefinite、语义重复 key、共享环、浮点收窄、截断 tag、断裂 chunk、未定义引用。</small></p>
</div>
{{else if eq .View "error"}}
<div class="card"><h2>导入失败</h2><p class="bad">{{.Err}}</p><p><a href="/">返回</a></p></div>
{{else if eq .View "item"}}
<div class="card">
  <h2>#{{.Item.ID}} {{.Item.Name}}
    <span class="switch" style="float:right">
      profile:
      <a {{if eq .Profile "rfc8949-deterministic"}}class="ok"{{end}} href="/item?id={{.Item.ID}}&profile=rfc8949-deterministic">RFC 8949 deterministic</a>
      <a {{if eq .Profile "none"}}class="ok"{{end}} href="/item?id={{.Item.ID}}&profile=none">仅宽松解析</a>
    </span>
  </h2>
  <div>{{.SVG}}</div>
</div>
<div class="cols">
  <div class="col card">
    <h3>① 原字节（不可变）</h3>
    <pre class="hex">{{range $i, $r := .Dump}}{{printf "%04x" $r.Offset}}  {{range $r.Cells}}<span style="background:{{.Color}}">{{.Byte}}</span> {{end}}
{{end}}</pre>
    <p class="digest">sha256: {{index .Digests "original"}}<br>len: {{index .Lengths "original"}} 字节</p>
    <p><small>导入后仅 INSERT OR IGNORE 写入，永不覆盖。</small></p>
  </div>
  <div class="col card">
    <h3>② 宽松解析树</h3>
    {{.Tree}}
    <h3 style="margin-top:10px">解码诊断（{{len .DecodeDiags}}）</h3>
    {{range .DecodeDiags}}<div class="diag {{.Severity}}"><code>[{{.Start}},{{.End}})</code> {{.Code}} — {{.Msg}}</div>{{else}}<small>无</small>{{end}}
    <p class="digest" style="margin-top:8px">lenient 重编码 sha256: {{index .Digests "lenient"}}
    {{if eq (index .Digests "lenient") (index .Digests "original")}}<br><span class="ok">与原字节逐字节一致（无损往返）</span>{{else}}<br><span class="bad">与原字节不一致</span>{{end}}</p>
  </div>
  <div class="col card">
  {{if eq .Profile "rfc8949-deterministic"}}
    <h3>③ 规范化候选</h3>
    <pre class="hex">{{hexBytes .Canon.Data}}</pre>
    <p class="digest">sha256: {{index .Digests "canonical"}}</p>
    <p>第二次规范化：{{if .Idempotent}}<span class="ok">幂等 ✓</span>{{else}}<span class="bad">不幂等 ✗</span>{{end}}
       <small>(digest {{.Re.Digest}})</small></p>
    <p>{{if .Canon.OK}}<span class="ok">所有子树编码成功</span>{{else}}<span class="bad">存在被替换的坏子树（相邻已确认项保留）</span>{{end}}</p>
    <h3 style="margin-top:8px">改写步骤（{{len .Canon.Steps}}）</h3>
    {{range .Canon.Steps}}<div class="step"><code>{{.Path}}</code> {{.Msg}}</div>{{else}}<small>无（已为规范形态）</small>{{end}}
    <h3 style="margin-top:10px">规范化诊断（{{len .Canon.Diags}}）</h3>
    {{range .Canon.Diags}}<div class="diag {{.Severity}}"><code>[{{.Start}},{{.End}})</code> {{.Code}} — {{.Msg}}</div>{{else}}<small>无</small>{{end}}
  {{else}}
    <h3>③ 规范化候选（已停用）</h3>
    <p><small>已选择「仅宽松解析」profile。<a href="/item?id={{.Item.ID}}&profile=rfc8949-deterministic">启用 RFC 8949 deterministic</a></small></p>
  {{end}}
  </div>
</div>
<p><a href="/">← 返回列表</a></p>
{{end}}
</main>
</body>
</html>`
