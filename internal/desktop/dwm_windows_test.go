//go:build windows

package desktop

import "testing"

func TestScaleToDPI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		length uint
		dpi    uint
		want   uint
	}{
		{"unchanged at 100 percent", 780, 96, 780},
		{"half again at 150 percent", 780, 144, 1170},
		{"rounded to nearest at 175 percent", 540, 168, 945},
		{"minimum width at 125 percent", 600, 120, 750},
		{"rounds half up", 1, 144, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := scaleToDPI(tt.length, tt.dpi); got != tt.want {
				t.Fatalf("scaleToDPI(%d, %d) = %d, want %d", tt.length, tt.dpi, got, tt.want)
			}
		})
	}
}

func TestSystemDPIIsPositive(t *testing.T) {
	t.Parallel()
	if dpi := loadUser32().systemDPI(); dpi < defaultDPI/2 {
		t.Fatalf("systemDPI() = %d, want a real display DPI", dpi)
	}
}

func TestCloseWithoutWindowReturnsAtOnce(t *testing.T) {
	t.Parallel()
	w := New("about:blank", "Ferry", t.TempDir(), nil)
	w.Close()
	if w.done != nil || w.isClosing {
		t.Fatalf("closing a window that was never shown left state behind: done=%v closing=%v", w.done, w.isClosing)
	}
}
