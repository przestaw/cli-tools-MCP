package vcs

import (
	"errors"
	"strings"
	"testing"
)

// fillByID is the smallest provider adapter: it reads the id from the raw details.
func fillByID(pr *PullRequest) { pr.ID = Number(pr.Details["id"]) }

func TestNormalizeAcceptsAllResponseShapes(t *testing.T) {
	cases := map[string]string{
		"array":           `[{"id":1,"title":"a"},{"id":2,"title":"b"}]`,
		"search envelope": `{"items":[{"id":1,"title":"a"},{"id":2,"title":"b"}]}`,
	}
	for name, body := range cases {
		res, err := Normalize("test", "o/r", []byte(body), fillByID)
		if err != nil || len(res.Items) != 2 {
			t.Fatalf("%s: %v, %d items", name, err, len(res.Items))
		}
		if first := res.Items[0]; first.Provider != "test" || first.Repository != "o/r" || first.Title != "a" || first.ID != 1 {
			t.Errorf("%s: first item = %+v", name, first)
		}
	}

	res, err := Normalize("test", "o/r", []byte(`{"id":7,"title":"one","draft":true}`), fillByID)
	if err != nil || len(res.Items) != 1 || res.Items[0].ID != 7 || !res.Items[0].Draft {
		t.Fatalf("single object: %v %+v", err, res.Items)
	}
}

func TestNormalizeRejectsBadInput(t *testing.T) {
	if _, err := Normalize("test", "o/r", []byte("not json"), fillByID); err == nil {
		t.Error("invalid JSON accepted")
	}
	_, err := Normalize("test", "o/r", []byte(`{"title":"no id"}`), fillByID)
	if err == nil || !strings.Contains(err.Error(), "pull request number") {
		t.Errorf("missing id: %v", err)
	}
	res, err := Normalize("test", "o/r", []byte(`[]`), fillByID)
	if err != nil || len(res.Items) != 0 || res.Items == nil {
		t.Errorf("empty list must give an empty, non-nil slice: %v %#v", err, res.Items)
	}
}

func TestSingle(t *testing.T) {
	requestErr := errors.New("request failed")
	if _, err := Single("test", "o/r", "not found", nil, requestErr, fillByID); !errors.Is(err, requestErr) {
		t.Errorf("request error not passed through: %v", err)
	}
	if _, err := Single("test", "o/r", "pull request 9 not found", []byte(`[]`), nil, fillByID); err == nil || err.Error() != "pull request 9 not found" {
		t.Errorf("empty result: %v", err)
	}
	pr, err := Single("test", "o/r", "not found", []byte(`{"id":3}`), nil, fillByID)
	if err != nil || pr.ID != 3 {
		t.Errorf("success: %v %+v", err, pr)
	}
}

func TestNumber(t *testing.T) {
	cases := []struct {
		in   any
		want int
	}{
		{float64(4), 4}, {float64(0), 0}, {1.5, 0}, {"4", 0}, {nil, 0},
	}
	for _, c := range cases {
		if got := Number(c.in); got != c.want {
			t.Errorf("Number(%#v) = %d, want %d", c.in, got, c.want)
		}
	}
}
