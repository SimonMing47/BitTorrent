package bencode

import (
	"bytes"
	"testing"
)

func TestParseAndMarshalRoundTrip(t *testing.T) {
	input := []byte("d3:cow3:moo4:spamli1ei2e4:eggsee")

	value, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	encoded, err := Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	if !bytes.Equal(input, encoded) {
		t.Fatalf("round trip mismatch:\nwant %q\ngot  %q", input, encoded)
	}
}

func TestMarshalSortsDictionaryKeys(t *testing.T) {
	encoded, err := Marshal(map[string]any{
		"z": []byte("last"),
		"a": int64(7),
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	expected := []byte("d1:ai7e1:z4:laste")
	if !bytes.Equal(expected, encoded) {
		t.Fatalf("unexpected encoding:\nwant %q\ngot  %q", expected, encoded)
	}
}

func TestParseRejectsTrailingData(t *testing.T) {
	if _, err := Parse([]byte("i1ee")); err == nil {
		t.Fatal("expected trailing data to fail")
	}
}
