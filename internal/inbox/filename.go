package inbox

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	fallbackFilename  = "file"
	maxFilenameBytes  = 255
	maxExtensionBytes = 32
	forbiddenRunes    = `<>:"/\|?*`
)

var reservedStem = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])$`)

func SanitizeFilename(name string) string {
	base := norm.NFC.String(lastPathSegment(strings.ToValidUTF8(name, "")))
	base = strings.Map(dropForbiddenRune, base)
	base = strings.Trim(base, " .")
	if base == "" {
		return fallbackFilename
	}
	if stem, _, _ := strings.Cut(base, "."); reservedStem.MatchString(stem) {
		base = "_" + base
	}
	return truncateFilename(base)
}

func lastPathSegment(name string) string {
	slashed := strings.ReplaceAll(name, `\`, "/")
	return slashed[strings.LastIndex(slashed, "/")+1:]
}

func dropForbiddenRune(r rune) rune {
	if unicode.IsControl(r) || strings.ContainsRune(forbiddenRunes, r) {
		return -1
	}
	return r
}

func truncateFilename(name string) string {
	if len(name) <= maxFilenameBytes {
		return name
	}
	ext := path.Ext(name)
	if len(ext) > maxExtensionBytes {
		ext = ""
	}
	stem := cutAtRuneBoundary(name[:len(name)-len(ext)], maxFilenameBytes-len(ext))
	stem = strings.TrimRight(stem, " .")
	if stem == "" {
		stem = fallbackFilename
	}
	return stem + ext
}

func numberedName(name string, n int) string {
	if n <= 1 {
		return name
	}
	suffix := " (" + strconv.Itoa(n) + ")"
	stem, ext := splitExtension(name)
	if len(name)+len(suffix) <= maxFilenameBytes {
		return stem + suffix + ext
	}
	if len(ext) > maxExtensionBytes {
		stem, ext = name, ""
	}
	return cutAtRuneBoundary(stem, maxFilenameBytes-len(suffix)-len(ext)) + suffix + ext
}

func cutAtRuneBoundary(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func splitExtension(name string) (stem, ext string) {
	ext = path.Ext(name)
	if ext == name {
		return name, ""
	}
	return name[:len(name)-len(ext)], ext
}
