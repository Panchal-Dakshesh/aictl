package sources

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// A schema-less protobuf reader. Antigravity stores conversations as protobuf
// blobs with no published schema, so we walk the wire format and keep every
// string and integer under its field path, e.g. "19.2" or "5.1.1".
type pbField struct {
	Path string
	Str  string
	Int  uint64
	IsS  bool
}

func pbFields(b []byte) []pbField {
	var out []pbField
	pbWalk(b, "", &out, 0)
	return out
}

func pbVarint(b []byte, i int) (uint64, int, bool) {
	var r uint64
	var s uint
	for i < len(b) {
		c := b[i]
		i++
		r |= uint64(c&0x7f) << s
		if c < 0x80 {
			return r, i, true
		}
		s += 7
		if s > 63 {
			return 0, i, false
		}
	}
	return 0, i, false
}

func pbWalk(b []byte, path string, out *[]pbField, depth int) bool {
	i := 0
	start := len(*out)
	for i < len(b) {
		k, n, ok := pbVarint(b, i)
		if !ok || k>>3 == 0 {
			*out = (*out)[:start]
			return false
		}
		i = n
		p := path + "." + strconv.FormatUint(k>>3, 10)
		switch k & 7 {
		case 0:
			v, n, ok := pbVarint(b, i)
			if !ok {
				*out = (*out)[:start]
				return false
			}
			i = n
			*out = append(*out, pbField{Path: p, Int: v})
		case 1:
			i += 8
		case 5:
			i += 4
		case 2:
			l, n, ok := pbVarint(b, i)
			if !ok || n+int(l) > len(b) {
				*out = (*out)[:start]
				return false
			}
			d := b[n : n+int(l)]
			i = n + int(l)
			if !looksLikeText(d) && depth < 12 && l > 1 && pbWalk(d, p, out, depth+1) {
				continue
			}
			if utf8.Valid(d) {
				*out = append(*out, pbField{Path: p, Str: string(d), IsS: true})
			}
		default:
			*out = (*out)[:start]
			return false
		}
		if i > len(b) {
			*out = (*out)[:start]
			return false
		}
	}
	return true
}

func pbStr(fs []pbField, path string) string {
	path = "." + path
	for _, f := range fs {
		if f.IsS && f.Path == path {
			return f.Str
		}
	}
	return ""
}

func pbInt(fs []pbField, path string) uint64 {
	path = "." + path
	for _, f := range fs {
		if !f.IsS && f.Path == path {
			return f.Int
		}
	}
	return 0
}

func pbHas(fs []pbField, prefix string) bool {
	prefix = "." + prefix
	for _, f := range fs {
		if strings.HasPrefix(f.Path, prefix) {
			return true
		}
	}
	return false
}

// looksLikeText guards against short strings that happen to parse as a
// nested message ("hi" is a valid varint field). Nested messages almost
// always contain control bytes; a leading '\n' is field 1's tag, so those
// still get a chance to parse as a message.
func looksLikeText(d []byte) bool {
	if len(d) == 0 || d[0] == '\n' || !utf8.Valid(d) {
		return false
	}
	for _, c := range d {
		if c < 0x20 && c != '\n' && c != '\t' && c != '\r' {
			return false
		}
	}
	return true
}
