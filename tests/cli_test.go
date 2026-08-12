package tests

import (
	"os/exec"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	cmd := exec.Command("go", "run", "..", "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("workctl version: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "0.1.1" {
		t.Fatalf("version output = %q", out)
	}
}
