package markdown

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSourceAndProjection(t *testing.T) {
	source := "# 中文😀\r\n\r\na  \r\nb\rc\n\n> **重点** 与 *强调*\n\n3. 项目\n   - 子项\n\n---\n\n```html\n<img src=x>\n```\n\n`<script>` \\<tag> &amp; \\&amp;\n\n[链接](https://example.test/a?q=1&amp;b=2)"
	normalized, err := Normalize(source, Format)
	if err != nil || !strings.Contains(normalized, "a  \nb\nc") {
		t.Fatalf("source normalization: %q %v", normalized, err)
	}
	view, err := Parse(source, Format)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(view)
	for _, kind := range []string{"heading", "break", "quote", "strong", "emphasis", "list", "rule", "code_block", "code", "link"} {
		if !strings.Contains(string(encoded), `"kind":"`+kind+`"`) {
			t.Errorf("missing %s: %s", kind, encoded)
		}
	}
	if decodeText([]byte(`\&amp; &amp; &#x1F600;`)) != "&amp; & 😀" {
		t.Fatal("CommonMark escaped entity changed")
	}
	empty, err := Parse("", Format)
	if err != nil || empty.Nodes == nil || len(empty.Nodes) != 0 {
		t.Fatal("empty document", err)
	}
}

func TestRejectedSource(t *testing.T) {
	for _, source := range []string{"<script>alert(1)</script>", "x <img src=x>", "![x](https://example.test/x)", "[x](javascript:alert)", "[x](jav&#x61;script:alert)", "[x](//example.test)", "[x](/relative)", "[x](https://user:password@example.test)", "[x](https://example.test/%0a)", "<a@example.test>", string([]byte{255}), "a\x00b", strings.Repeat("x", MaxBytes+1), strings.Repeat("> ", 40) + "deep"} {
		t.Run(source[:min(len(source), 40)], func(t *testing.T) {
			_, err := Parse(source, Format)
			var diagnostic *Diagnostic
			if !errors.As(err, &diagnostic) {
				t.Fatalf("expected bounded diagnostic: %v", err)
			}
			if strings.Contains(err.Error(), "alert") {
				t.Fatal("source leaked")
			}
		})
	}
	if _, err := Parse("x", "unknown"); err == nil {
		t.Fatal("format accepted")
	}
	for _, value := range []string{"https://", "https:example.test", "https://example.test\\evil", "https://example.test/%5c", "https://example.test/\t", "mailto:a@example.test", "entity://document/doc_x"} {
		if ValidURL(value) {
			t.Errorf("accepted %q", value)
		}
	}
}

func TestBoundedAdversarialInputs(t *testing.T) {
	for _, source := range []string{strings.Repeat("x", MaxBytes), strings.Repeat("*_", MaxBytes/2), strings.Repeat("[", MaxBytes), strings.Repeat("a\n\n", MaxBytes/3), strings.Repeat(">", MaxBytes)} {
		_, _ = Parse(source, Format)
	}
}

func BenchmarkMaximumInput(b *testing.B) {
	for name, source := range map[string]string{"text": strings.Repeat("x", MaxBytes), "delimiters": strings.Repeat("*_", MaxBytes/2), "brackets": strings.Repeat("[", MaxBytes), "nesting": strings.Repeat(">", MaxBytes)} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _ = Parse(source, Format)
			}
		})
	}
}
