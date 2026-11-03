// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"workctl/internal/config"
)

func TestStreamCopiesBinaryUnchanged(t *testing.T) {
	payload := []byte{0x1f, 0x8b, 0x08, 0x00, 0xff, 0x00, 0x7b, 0x22} // gzip magic, a NUL and a "{" that is not JSON
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/7" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="service.log.gz"`)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	c, err := newClient(config.Profile{URL: srv.URL, VerifyTLS: true}, "", time.Second)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	meta, err := c.Stream(context.Background(), "/files/7", nil, &out)
	sum := sha256.Sum256(payload)
	if err != nil || !bytes.Equal(out.Bytes(), payload) {
		t.Fatalf("stream = %x, %v", out.Bytes(), err)
	}
	want := Streamed{Bytes: int64(len(payload)), SHA256: hex.EncodeToString(sum[:]), ContentType: "application/gzip", ServerFileName: "service.log.gz"}
	if meta != want {
		t.Fatalf("meta = %+v\nwant   %+v", meta, want)
	}

	out.Reset()
	_, err = c.Stream(context.Background(), "/files/missing", nil, &out)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound || out.Len() != 0 {
		t.Fatalf("missing file: %v, wrote %d bytes", err, out.Len())
	}
}
