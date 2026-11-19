package pr

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestFormatCompactIsTheDefault(t *testing.T) {
	cfg := gitlabConfig(t)
	call := func(tool, format string, include []any) (string, error) {
		a := map[string]any{"profile": "gl", "number": 1, "format": format}
		if include != nil {
			a["include"] = include
		}
		out, err := CallTool(context.Background(), cfg, tool, a)
		return string(out), err
	}
	for _, tool := range []string{"pr_list", "pr_show"} {
		for _, include := range [][]any{nil, {"checks"}} {
			t.Run(tool+"/include="+strings.Join(anyStrings(include), ","), func(t *testing.T) {
				def, err := call(tool, "", include)
				if err != nil || strings.Contains(def, "sentinel") {
					t.Fatalf("default: %s %v", def, err)
				}
				for _, format := range []string{"compact", "COMPACT", "json"} {
					got, err := call(tool, format, include)
					if err != nil || got != def {
						t.Fatalf("%s differs from default: %s %v", format, got, err)
					}
				}
				raw, err := call(tool, "raw", include)
				if err != nil || raw == def || !strings.Contains(raw, "sentinel") {
					t.Fatalf("raw: %s %v", raw, err)
				}
				_, err = call(tool, "bogus", include)
				if err == nil {
					t.Fatal("bogus accepted")
				}
				for _, want := range []string{"compact", "raw", "json", "text"} {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("error does not name %q: %v", want, err)
					}
				}
			})
		}
	}
}

func anyStrings(in []any) []string {
	var out []string
	for _, v := range in {
		out = append(out, v.(string))
	}
	return out
}

func TestCLIFormatCompactEqualsDefaultJSON(t *testing.T) {
	cfg := gitlabConfig(t)
	run := func(args ...string) string {
		f, err := os.CreateTemp(t.TempDir(), "stdout")
		if err != nil {
			t.Fatal(err)
		}
		old := os.Stdout
		os.Stdout = f
		defer func() { os.Stdout = old; _ = f.Close() }()
		if err := Command(cfg, args); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(f.Name())
		return string(b)
	}
	for _, action := range []string{"list", "show"} {
		tail := []string{"--profile", "gl"}
		if action == "show" {
			tail = append(tail, "1")
		}
		want, err := CallTool(context.Background(), cfg, "pr_"+action, map[string]any{"profile": "gl", "number": 1})
		if err != nil {
			t.Fatal(err)
		}
		for _, format := range []string{"compact", "COMPACT", "json"} {
			got := run(append([]string{action, "--format", format}, tail...)...)
			if strings.TrimSpace(got) != strings.TrimSpace(string(want)) {
				t.Fatalf("%s --format %s:\n%s\nwant:\n%s", action, format, got, want)
			}
		}
		if text := run(append([]string{action}, tail...)...); strings.HasPrefix(strings.TrimSpace(text), "{") || strings.HasPrefix(strings.TrimSpace(text), "[") {
			t.Fatalf("%s default CLI output is no longer text: %s", action, text)
		}
	}
}
