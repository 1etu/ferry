package inbox

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeFilename(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain name is kept", "IMG_0001.HEIC", "IMG_0001.HEIC"},
		{"only the last path segment survives", "photos/2026/IMG_0001.HEIC", "IMG_0001.HEIC"},
		{"backslashes are path separators", `C:\Users\me\report.pdf`, "report.pdf"},
		{"trailing slash leaves an empty segment", "photos/", "file"},
		{"decomposed accents become NFC", "Cafe\u0301.txt", "Café.txt"},
		{"control characters are dropped", "a\x00b\x1fc\x7f.txt", "abc.txt"},
		{"windows-forbidden characters are dropped", `a<b>c:d"e|f?g*h.txt`, "abcdefgh.txt"},
		{"leading and trailing spaces and dots are trimmed", "  ..notes.txt. . ", "notes.txt"},
		{"dots only becomes file", "...", "file"},
		{"empty becomes file", "", "file"},
		{"reserved device name gets a prefix", "CON", "_CON"},
		{"reserved device name with extension gets a prefix", "com1.txt", "_com1.txt"},
		{"reserved stem before the first dot gets a prefix", "LPT9.tar.gz", "_LPT9.tar.gz"},
		{"reserved name as a longer stem is fine", "CONSOLE.txt", "CONSOLE.txt"},
		{"long name is cut to 255 bytes keeping the extension", strings.Repeat("a", 300) + ".jpeg", strings.Repeat("a", 250) + ".jpeg"},
		{"long extension is not kept", strings.Repeat("a", 300) + "." + strings.Repeat("x", 40), strings.Repeat("a", 255)},
		{"cut lands on a rune boundary", strings.Repeat("é", 200) + ".txt", strings.Repeat("é", 125) + ".txt"},
		{"cut does not leave a trailing dot", strings.Repeat("b", 253) + "." + strings.Repeat("c", 10) + ".md", strings.Repeat("b", 252) + ".md"},
		{"invalid utf-8 bytes are dropped", "a\xffb\xc3.txt", "ab.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := SanitizeFilename(tc.in)
			if got != tc.want {
				t.Fatalf("SanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if len(got) > maxFilenameBytes || !utf8.ValidString(got) {
				t.Fatalf("result %q is %d bytes or invalid UTF-8", got, len(got))
			}
		})
	}
}

func TestNumberedName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"first attempt is the name itself", "photo.jpg", 1, "photo.jpg"},
		{"suffix goes before the last extension", "archive.tar.gz", 2, "archive.tar (2).gz"},
		{"no extension", "README", 3, "README (3)"},
		{"full-length stem shrinks to fit the suffix", strings.Repeat("a", 250) + ".jpeg", 2, strings.Repeat("a", 246) + " (2).jpeg"},
		{"stem shrinks at a rune boundary", strings.Repeat("é", 125) + ".txt", 2, strings.Repeat("é", 123) + " (2).txt"},
		{"long extension is part of the stem when shrinking", "a." + strings.Repeat("x", 250), 2, "a." + strings.Repeat("x", 249) + " (2)"},
		{"wide counters shrink the stem further", strings.Repeat("a", 251) + ".md", 10000, strings.Repeat("a", 244) + " (10000).md"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := numberedName(tc.in, tc.n)
			if got != tc.want {
				t.Fatalf("numberedName(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
			if len(got) > maxFilenameBytes || !utf8.ValidString(got) {
				t.Fatalf("result %q is %d bytes or invalid UTF-8", got, len(got))
			}
		})
	}
}

func TestSplitExtension(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, stem, ext string
	}{
		{"photo.jpg", "photo", ".jpg"},
		{"archive.tar.gz", "archive.tar", ".gz"},
		{"README", "README", ""},
		{"name.", "name", "."},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			stem, ext := splitExtension(tc.in)
			if stem != tc.stem || ext != tc.ext {
				t.Fatalf("splitExtension(%q) = %q, %q; want %q, %q", tc.in, stem, ext, tc.stem, tc.ext)
			}
		})
	}
}
