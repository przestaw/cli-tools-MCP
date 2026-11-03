// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package output

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestTableFieldsAndCells(t *testing.T) {
	for _, fields := range [][]string{{" status ", "key"}, {"missing"}, nil} {
		var buf bytes.Buffer
		p := &Printer{W: &buf, Fields: fields}
		if err := p.Table([]string{"Key", "Status"}, [][]string{{"A-1", "a\nb\tc"}, {"A-2"}}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "A-1") || !strings.Contains(buf.String(), "a b c") {
			t.Fatal(buf.String())
		}
		if len(fields) == 2 && !strings.HasPrefix(buf.String(), "STATUS") {
			t.Fatal(buf.String())
		}
	}
	p := &Printer{}
	long := strings.Repeat("\u0105", 100)
	if len([]rune(p.Cell(long))) != 80 {
		t.Fatal("invalid rune truncation")
	}
	p.Expand = true
	if p.Cell(long) != long {
		t.Fatal("expand ignored")
	}
}

func TestRawAndKeyValue(t *testing.T) {
	var buf bytes.Buffer
	p := &Printer{W: &buf}
	if err := p.Raw([]byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "{\n  \"a\": 1\n}\n" {
		t.Fatal(buf.String())
	}
	buf.Reset()
	if err := p.Raw([]byte("plain")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "plain" {
		t.Fatal(buf.String())
	}
	buf.Reset()
	if err := p.KeyValue([][2]string{{"Title", "One"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "FIELD") || !strings.Contains(buf.String(), "One") {
		t.Fatal(buf.String())
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestWriterErrors(t *testing.T) {
	p := &Printer{W: failedWriter{}}
	for _, err := range []error{p.Table([]string{"Key"}, [][]string{{"A-1"}}), p.Raw([]byte("plain")), p.JSONValue("value")} {
		if err == nil {
			t.Fatal("write error lost")
		}
	}
}
