package app

import (
	"sort"
	"strings"

	"go.planetmeican.com/yangguang/postkid/internal/model"
)

// ExportCurl 把 ResolvedRequest 转为 curl 命令字符串。
func ExportCurl(r model.ResolvedRequest) string {
	var b strings.Builder

	b.WriteString("curl")

	// 非 GET 或带 body 时显式指定 method
	if r.Method != "GET" || r.Body != "" {
		b.WriteString(" -X ")
		b.WriteString(r.Method)
	}

	// URL（用单引号避免 shell 转义问题）
	b.WriteString(" '")
	b.WriteString(escapeShell(r.URL))
	b.WriteString("'")

	// Headers
	keys := make([]string, 0, len(r.Headers))
	for k := range r.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(" \\\n  -H '")
		b.WriteString(escapeShell(k))
		b.WriteString(": ")
		b.WriteString(escapeShell(r.Headers[k]))
		b.WriteString("'")
	}

	// Body
	if r.Body != "" {
		b.WriteString(" \\\n  -d '")
		b.WriteString(escapeShell(r.Body))
		b.WriteString("'")
	}

	return b.String()
}

// escapeShell 对单引号内的文本做转义：单引号替换为 '\''（shell 单引号拼接技巧）。
func escapeShell(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}