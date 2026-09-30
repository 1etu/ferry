package outbox

import (
	"net/http/httptest"
	"testing"
)

func TestContentDisposition(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		disposition string
		filename    string
		want        string
	}{
		{"ascii name", "", "report.pdf", `attachment; filename="report.pdf"; filename*=UTF-8''report.pdf`},
		{"spaces are percent encoded", "attachment", "my report.pdf", `attachment; filename="my report.pdf"; filename*=UTF-8''my%20report.pdf`},
		{"utf-8 name keeps one placeholder per rune", "", "Fotoğraf.jpg", `attachment; filename="Foto_raf.jpg"; filename*=UTF-8''Foto%C4%9Fraf.jpg`},
		{"cjk name", "", "日本.txt", `attachment; filename="__.txt"; filename*=UTF-8''%E6%97%A5%E6%9C%AC.txt`},
		{"quotes and backslashes cannot break the header", "", `a"b\c.txt`, `attachment; filename="a_b_c.txt"; filename*=UTF-8''a%22b%5Cc.txt`},
		{"control characters are replaced", "", "a\r\nb.txt", `attachment; filename="a__b.txt"; filename*=UTF-8''a%0D%0Ab.txt`},
		{"inline on request", "inline", "clip.mp4", `inline; filename="clip.mp4"; filename*=UTF-8''clip.mp4`},
		{"unknown disposition falls back to attachment", "bogus", "a.txt", `attachment; filename="a.txt"; filename*=UTF-8''a.txt`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := contentDisposition(tt.disposition, tt.filename); got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestServeInlineDispositionAndUnknownType(t *testing.T) {
	t.Parallel()
	fx := newFixture(t)
	f := fx.offer(t, "blob.wmtunknown", []byte("x"))
	r := contentRequest(t, f.ID, nil)
	r.URL.RawQuery = "disposition=inline"
	w := httptest.NewRecorder()

	fx.outbox.Serve(w, r, f.ID)

	if got := w.Header().Get("Content-Disposition"); got != `inline; filename="blob.wmtunknown"; filename*=UTF-8''blob.wmtunknown` {
		t.Fatalf("got disposition %q", got)
	}
	if got := w.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("got content type %q, want application/octet-stream", got)
	}
}
