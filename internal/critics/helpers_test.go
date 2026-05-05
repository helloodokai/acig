package critics

import "testing"

func TestStripMarkdownFence(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"no fence",
			`{"findings": []}`,
			`{"findings": []}`,
		},
		{
			"json fence",
			"```json\n{\"findings\": []}\n```",
			`{"findings": []}`,
		},
		{
			"plain fence",
			"```\n{\"findings\": []}\n```",
			`{"findings": []}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripMarkdownFence(tt.input)
			if got != tt.want {
				t.Errorf("stripMarkdownFence(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	if truncate("hello", 10) != "hello" {
		t.Error("should not truncate short strings")
	}
	if truncate("hello world this is long", 10) != "hello worl..." {
		t.Error("should truncate long strings with ellipsis")
	}
}