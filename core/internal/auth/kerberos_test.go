package auth

import (
	"os"
	"testing"
	"time"

	"github.com/jcmturner/gokrb5/v8/keytab"
)

func TestDecode_NegotiateToken(t *testing.T) {
	raw, ok := Decode_NegotiateToken("Negotiate bm90LWEtdGlja2V0")
	if !ok || string(raw) != "not-a-ticket" {
		t.Fatalf("token %q %v", raw, ok)
	}
	if _, ok = Decode_NegotiateToken("Bearer bm90"); ok {
		t.Fatal("bearer accepted")
	}
	if _, ok = Decode_NegotiateToken(""); ok {
		t.Fatal("empty accepted")
	}
}

func TestAccept_NegotiateTokenRejectsGarbage(t *testing.T) {
	loaded := keytab.New()
	if err := loaded.AddEntry("HTTP/docs", "BANK.LOCAL", "password", time.Now(), 1, 18); err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(t.TempDir(), "docs.keytab")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = loaded.Write(file); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = Accept_NegotiateToken(file.Name(), "HTTP/docs@BANK.LOCAL", []byte("not-a-ticket")); err == nil {
		t.Fatal("garbage ticket accepted")
	}
	if _, err = Accept_NegotiateToken("", "HTTP/docs@BANK.LOCAL", []byte("not-a-ticket")); err == nil {
		t.Fatal("missing keytab accepted")
	}
}
