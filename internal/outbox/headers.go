package outbox

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/1etu/ferry/internal/store"
)

const upperHex = "0123456789ABCDEF"

func setContentHeaders(h http.Header, f store.File, info os.FileInfo, disposition string) {
	h.Set("Content-Disposition", contentDisposition(disposition, f.Name))
	h.Set("Content-Type", contentType(f.Name))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-store")
	h.Set("ETag", strongETag(info))
}

func strongETag(info os.FileInfo) string {
	return fmt.Sprintf("%q", strconv.FormatInt(info.Size(), 16)+"-"+strconv.FormatInt(info.ModTime().UnixNano(), 16))
}

func contentType(name string) string {
	if t := mime.TypeByExtension(filepath.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

func contentDisposition(disposition, name string) string {
	kind := "attachment"
	if disposition == "inline" {
		kind = "inline"
	}
	return kind + `; filename="` + asciiFilename(name) + `"; filename*=UTF-8''` + percentEncode(name)
}

func asciiFilename(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r >= 0x7f || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
}

func percentEncode(name string) string {
	var b strings.Builder
	for i := range len(name) {
		c := name[i]
		if isAttrChar(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(upperHex[c>>4])
		b.WriteByte(upperHex[c&0x0f])
	}
	return b.String()
}

func isAttrChar(c byte) bool {
	switch {
	case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9':
		return true
	default:
		return strings.IndexByte("!#$&+-.^_`|~", c) >= 0
	}
}
