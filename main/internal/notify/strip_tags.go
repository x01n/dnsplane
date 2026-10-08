package notify

import "strings"

func stripTags(s string) string {
	if !strings.ContainsRune(s, '<') {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	tagStart := -1 // 当前标签的起始下标，-1 表示不在标签态
	depth := 0     // 标签态内的 '<' 嵌套深度
	var quote byte // 当前引号字符，0 表示不在引号态

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if tagStart < 0 {
			if ch != '<' {
				b.WriteByte(ch)
				continue
			}
			// 判定是否构成标签起始：'<' 后为空白或已到结尾时不算标签，'<' 原样保留
			if i+1 < len(s) && isStripTagsSpace(s[i+1]) {
				b.WriteByte(ch)
				continue
			}
			tagStart = i
			depth = 1
			quote = 0
			continue
		}

		// 标签态内
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			quote = ch
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				tagStart = -1
			}
		}
	}

	// 未闭合标签：丢弃起始 '<' 之后的全部内容；否则返回已累积的正文
	return b.String()
}

func isStripTagsSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}
