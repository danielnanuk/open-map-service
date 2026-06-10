// Apply 实现 X-Goog-FieldMask 语义的响应裁剪:逗号分隔的点路径,数组元素逐个应用。
// 与 Google 的差异(刻意放宽):缺失 header 时返回全量而非报错。
package fieldmask

import "strings"

type node map[string]node

func Apply(doc map[string]any, mask string) map[string]any {
	mask = strings.TrimSpace(mask)
	if mask == "" || mask == "*" {
		return doc
	}
	root := node{}
	for _, path := range strings.Split(mask, ",") {
		cur := root
		for _, part := range strings.Split(strings.TrimSpace(path), ".") {
			if cur[part] == nil {
				cur[part] = node{}
			}
			cur = cur[part]
		}
	}
	return pruneMap(doc, root)
}

func pruneMap(m map[string]any, n node) map[string]any {
	out := map[string]any{}
	for key, sub := range n {
		val, ok := m[key]
		if !ok {
			continue
		}
		if len(sub) == 0 { // 叶子:整棵保留
			out[key] = val
			continue
		}
		out[key] = pruneValue(val, sub)
	}
	return out
}

func pruneValue(v any, n node) any {
	switch tv := v.(type) {
	case map[string]any:
		return pruneMap(tv, n)
	case []any:
		arr := make([]any, 0, len(tv))
		for _, item := range tv {
			arr = append(arr, pruneValue(item, n))
		}
		return arr
	default:
		return v
	}
}
