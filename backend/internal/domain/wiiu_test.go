package domain

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// wua builds a ZArchive whose root holds folders named roots, with no file data.
func wua(roots ...string) []byte {
	var names, tree bytes.Buffer
	nameAt := func(s string) uint32 {
		at := uint32(names.Len())
		names.WriteByte(byte(len(s)))
		names.WriteString(s)
		return at
	}
	entry := func(head, a, b uint32) {
		_ = binary.Write(&tree, binary.BigEndian, [4]uint32{head, a, b, 0})
	}
	entry(nameAt(""), 1, uint32(len(roots)))
	for _, r := range roots {
		entry(nameAt(r), 0, 0)
	}
	var out bytes.Buffer
	out.WriteString("data")
	namesAt := uint64(out.Len())
	out.Write(names.Bytes())
	treeAt := uint64(out.Len())
	out.Write(tree.Bytes())
	end := uint64(out.Len())
	sections := [6][2]uint64{{0, 4}, {namesAt, 0}, {namesAt, uint64(names.Len())}, {treeAt, uint64(tree.Len())}, {end, 0}, {end, 0}}
	_ = binary.Write(&out, binary.BigEndian, sections)
	out.Write(make([]byte, 32))
	_ = binary.Write(&out, binary.BigEndian, uint64(end+144))
	_ = binary.Write(&out, binary.BigEndian, uint32(0x61BF3A01))
	_ = binary.Write(&out, binary.BigEndian, uint32(zarchiveMagic))
	return out.Bytes()
}

func TestWUATitleID(t *testing.T) {
	cases := map[string]struct {
		roots []string
		want  uint64
		ok    bool
	}{
		"game and update": {[]string{"0005000e10180600_v16", "0005000010180600_v0"}, 0x0005000010180600, true},
		"update only":     {[]string{"0005000e10180600_v16"}, 0, false},
		"not titles":      {[]string{"code", "content"}, 0, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := wua(c.roots...)
			got, err := WUATitleID(bytes.NewReader(b), int64(len(b)))
			if (err == nil) != c.ok || got != c.want {
				t.Fatalf("got %016X, %v", got, err)
			}
		})
	}
	if _, err := WUATitleID(bytes.NewReader(make([]byte, 200)), 200); err == nil {
		t.Error("a file that isn't a ZArchive was read")
	}
}

func TestWiiUMetaTitleID(t *testing.T) {
	xml := []byte(`<?xml version="1.0"?><menu><title_id type="hexBinary" length="8">0005000010180600</title_id></menu>`)
	if got, err := WiiUMetaTitleID(xml); err != nil || got != 0x0005000010180600 {
		t.Fatalf("got %016X, %v", got, err)
	}
	if got := WiiUTitleDir("/save", 0x0005000010180600); got != "/save/00050000/10180600" {
		t.Fatalf("got %s", got)
	}
}
