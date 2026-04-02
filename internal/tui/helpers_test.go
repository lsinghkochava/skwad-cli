package tui

import "testing"

func TestTruncate_UnderLimit(t *testing.T) {
	got := truncate("hello", 10)
	if got != "hello" {
		t.Errorf("truncate(%q, 10) = %q, want %q", "hello", got, "hello")
	}
}

func TestTruncate_AtLimit(t *testing.T) {
	got := truncate("hello", 5)
	if got != "hello" {
		t.Errorf("truncate(%q, 5) = %q, want %q", "hello", got, "hello")
	}
}

func TestTruncate_OverLimit(t *testing.T) {
	got := truncate("hello world", 5)
	want := "hell…"
	if got != want {
		t.Errorf("truncate(%q, 5) = %q, want %q", "hello world", got, want)
	}
}

func TestTruncate_Empty(t *testing.T) {
	got := truncate("", 10)
	if got != "" {
		t.Errorf("truncate(%q, 10) = %q, want %q", "", got, "")
	}
}

func TestTruncate_SingleChar(t *testing.T) {
	got := truncate("abcdef", 1)
	want := "…"
	if got != want {
		t.Errorf("truncate(%q, 1) = %q, want %q", "abcdef", got, want)
	}
}

func TestTruncate_UTF8_Emoji(t *testing.T) {
	// 4 emoji runes, truncate to 3 → 2 emoji + "…"
	input := "🔥🚀💡🎯"
	got := truncate(input, 3)
	want := "🔥🚀…"
	if got != want {
		t.Errorf("truncate(emoji, 3) = %q, want %q", got, want)
	}
}

func TestTruncate_UTF8_CJK(t *testing.T) {
	input := "你好世界测试"
	got := truncate(input, 4)
	want := "你好世…"
	if got != want {
		t.Errorf("truncate(CJK, 4) = %q, want %q", got, want)
	}
}

func TestTruncate_UTF8_MixedASCIIAndMultibyte(t *testing.T) {
	input := "ab🔥cd"
	got := truncate(input, 4)
	want := "ab🔥…"
	if got != want {
		t.Errorf("truncate(mixed, 4) = %q, want %q", got, want)
	}
}

func TestTruncate_UTF8_UnderLimit(t *testing.T) {
	input := "🔥🚀"
	got := truncate(input, 5)
	if got != input {
		t.Errorf("truncate(emoji under limit) = %q, want %q", got, input)
	}
}
