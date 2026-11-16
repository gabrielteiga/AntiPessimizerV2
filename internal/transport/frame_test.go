package transport

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

type fragment struct{ io.Reader }

func (f fragment) Read(b []byte) (int, error) {
	if len(b) > 1 {
		b = b[:1]
	}
	return f.Reader.Read(b)
}
func TestFraming(t *testing.T) {
	var b bytes.Buffer
	in := map[string]int{"value": 42}
	if err := Write(&b, in); err != nil {
		t.Fatal(err)
	}
	data := append([]byte(nil), b.Bytes()...)
	var out map[string]int
	if err := Read(fragment{&b}, &out); err != nil || out["value"] != 42 {
		t.Fatal(out, err)
	}
	for n := 1; n < len(data); n++ {
		if err := Read(bytes.NewReader(data[:n]), &out); err == nil {
			t.Fatal("accepted truncated", n)
		}
	}
	var h [4]byte
	binary.LittleEndian.PutUint32(h[:], MaxFrame+1)
	if err := Read(bytes.NewReader(h[:]), &out); err == nil {
		t.Fatal("oversize accepted")
	}
}
