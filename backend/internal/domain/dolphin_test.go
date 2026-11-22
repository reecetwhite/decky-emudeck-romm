package domain

import (
	"encoding/binary"
	"path/filepath"
	"testing"
)

// discImage is the start of a disc image in a container whose disc header is at at.
func discImage(magic string, at int, id string, disc byte, wii bool) []byte {
	b := make([]byte, DolphinHeaderSize)
	copy(b, magic)
	copy(b[at:], id)
	b[at+6] = disc
	if wii {
		binary.BigEndian.PutUint32(b[at+0x18:], wiiMagic)
	} else {
		binary.BigEndian.PutUint32(b[at+0x1C:], gcMagic)
	}
	return b
}

func TestParseDolphinDisc(t *testing.T) {
	cases := map[string]struct {
		image []byte
		want  DolphinDisc
	}{
		"rvz Wii":       {discImage("RVZ\x01", 0x58, "RMGE01", 0, true), DolphinDisc{ID: "RMGE01", Wii: true}},
		"rvz GameCube":  {discImage("RVZ\x01", 0x58, "GK7E08", 1, false), DolphinDisc{ID: "GK7E08", Disc: 1}},
		"wia":           {discImage("WIA\x01", 0x58, "SB4E01", 0, true), DolphinDisc{ID: "SB4E01", Wii: true}},
		"wbfs":          {discImage("WBFS", 0x200, "SMNE01", 0, true), DolphinDisc{ID: "SMNE01", Wii: true}},
		"ciso":          {discImage("CISO", 0x8000, "GMSE01", 0, false), DolphinDisc{ID: "GMSE01"}},
		"plain Wii iso": {discImage("", 0, "R8PE01", 0, true), DolphinDisc{ID: "R8PE01", Wii: true}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ParseDolphinDisc(c.image)
			if err != nil || got != c.want {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
	gcz := make([]byte, DolphinHeaderSize)
	binary.LittleEndian.PutUint32(gcz, 0xB10BC001)
	if _, err := ParseDolphinDisc(gcz); err == nil {
		t.Error("a .gcz was read")
	}
	if _, err := ParseDolphinDisc(make([]byte, DolphinHeaderSize)); err == nil {
		t.Error("an image with no disc magic was read")
	}
}

func TestWiiTitleDir(t *testing.T) {
	got := WiiTitleDir("/nand", DolphinDisc{ID: "RMGE01", Wii: true})
	if want := filepath.Join("/nand", "title", "00010000", "524d4745"); got != want {
		t.Fatalf("got %s", got)
	}
}
