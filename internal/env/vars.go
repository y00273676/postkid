// Package env 负责 {{variable}} 变量的合并与替换。
package env

import (
	"maps"
	"regexp"
)

// varPattern 匹配 {{name}}，name 限字母数字下划线。
var varPattern = regexp.MustCompile(`\{\{(\w+)\}\}`)

// Merge 按优先级合并变量：request > collection > environment（前者覆盖后者）。
// 任意一层为 nil 时跳过。
func Merge(requestVars, collectionVars, envVars map[string]string) map[string]string {
	out := make(map[string]string)
	maps.Copy(out, envVars)
	maps.Copy(out, collectionVars)
	maps.Copy(out, requestVars)
	return out
}

// Substitute 把 s 中所有 {{key}} 替换为 vars[key]。
// 返回替换后的字符串和未命中的变量名列表（保留原 {{key}} 文本）。
func Substitute(s string, vars map[string]string) (string, []string) {
	var missing []string
	result := varPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-2] // 去掉 {{ }}
		if v, ok := vars[name]; ok {
			return v
		}
		missing = append(missing, name)
		return match
	})
	return result, missing
}
