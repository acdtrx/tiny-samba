package userdb

import "testing"

func TestNTHash_KnownVector(t *testing.T) {
	// Well-known test vector: NT hash of "password" is 8846F7EAEE8FB117AD06BDD830B7586C.
	got := NTHash("password")
	want := "8846F7EAEE8FB117AD06BDD830B7586C"
	if got != want {
		t.Errorf("NTHash(\"password\") = %s, want %s", got, want)
	}
}

func TestNTHash_EmptyString(t *testing.T) {
	// NT hash of empty string is 31D6CFE0D16AE931B73C59D7E0C089C0 (well-known).
	got := NTHash("")
	want := "31D6CFE0D16AE931B73C59D7E0C089C0"
	if got != want {
		t.Errorf("NTHash(\"\") = %s, want %s", got, want)
	}
}

func TestNormalizeHash_Plaintext(t *testing.T) {
	got, err := NormalizeHash("password")
	if err != nil {
		t.Fatal(err)
	}
	if got != "8846F7EAEE8FB117AD06BDD830B7586C" {
		t.Errorf("got %s", got)
	}
}

func TestNormalizeHash_PreHashed(t *testing.T) {
	got, err := NormalizeHash("$NT$8846F7EAEE8FB117AD06BDD830B7586C")
	if err != nil {
		t.Fatal(err)
	}
	if got != "8846F7EAEE8FB117AD06BDD830B7586C" {
		t.Errorf("got %s", got)
	}
}

func TestNormalizeHash_PreHashedLowercase(t *testing.T) {
	got, err := NormalizeHash("$NT$8846f7eaee8fb117ad06bdd830b7586c")
	if err != nil {
		t.Fatal(err)
	}
	if got != "8846F7EAEE8FB117AD06BDD830B7586C" {
		t.Errorf("got %s", got)
	}
}

func TestNormalizeHash_BadLength(t *testing.T) {
	_, err := NormalizeHash("$NT$abcd")
	if err == nil {
		t.Fatal("expected error for short hash")
	}
}

func TestNormalizeHash_BadHex(t *testing.T) {
	_, err := NormalizeHash("$NT$ZZZZF7EAEE8FB117AD06BDD830B7586C")
	if err == nil {
		t.Fatal("expected error for non-hex hash")
	}
}
