package xiaohongshu

import "testing"

func TestAcceptsImage(t *testing.T) {
	cases := []struct {
		name   string
		accept string
		want   bool
	}{
		{"图片扩展名", ".png,.webp", true},
		{"大写扩展名", ".JPG", true},
		{"MIME 通配", "image/*", true},
		{"非图片扩展名", ".xyz,.abc", false},
		{"非图片 MIME", "video/mp4", false},
		{"空值", "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := acceptsImage(c.accept); got != c.want {
				t.Errorf("acceptsImage(%q) = %t，期望 %t", c.accept, got, c.want)
			}
		})
	}
}
