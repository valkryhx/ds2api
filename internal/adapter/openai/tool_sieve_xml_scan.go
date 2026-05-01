package openai

import "ds2api/internal/util"

func findFirstToolMarkupTagByName(s string, start int, name string) (util.ToolMarkupTag, bool) {
	return findFirstToolMarkupTagByNameFrom(s, start, name, false)
}

func findFirstToolMarkupTagByNameFrom(s string, start int, name string, closing bool) (util.ToolMarkupTag, bool) {
	for pos := maxInt(start, 0); pos < len(s); {
		tag, ok := util.FindToolMarkupTagOutsideIgnored(s, pos)
		if !ok {
			return util.ToolMarkupTag{}, false
		}
		if tag.Name == name && tag.Closing == closing {
			return tag, true
		}
		pos = tag.End + 1
	}
	return util.ToolMarkupTag{}, false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

