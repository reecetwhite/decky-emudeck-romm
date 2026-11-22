package domain

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
)

// DolphinHeaderSize is how much of a disc image ParseDolphinDisc reads: enough to reach the disc
// header in every format Dolphin plays that keeps it uncompressed.
const DolphinHeaderSize = 0x8100

const (
	wiiMagic = 0x5D1C9EA3
	gcMagic  = 0xC2339F3D
)

// DolphinDisc is the game on a GameCube or Wii disc, as its disc header names it.
type DolphinDisc struct {
	// ID is the six-character game ID: the four-character title code and the two-character maker.
	ID string
	// Disc is the disc's number, 0 for the first.
	Disc int
	Wii  bool
}

// ParseDolphinDisc reads the disc header from the start of a disc image: a plain image (.iso,
// .gcm), or a .rvz, .wia, .wbfs or .ciso, which keep the header uncompressed at their own offsets.
func ParseDolphinDisc(header []byte) (DolphinDisc, error) {
	at := 0
	switch {
	case len(header) >= 4 && (string(header[:4]) == "RVZ\x01" || string(header[:4]) == "WIA\x01"):
		// After the 0x48-byte file header, the disc struct keeps the disc header 0x10 bytes in.
		at = 0x58
	case len(header) >= 4 && string(header[:4]) == "WBFS":
		at = 0x200
	case len(header) >= 4 && string(header[:4]) == "CISO":
		at = 0x8000
	case len(header) >= 4 && binary.LittleEndian.Uint32(header) == 0xB10BC001:
		return DolphinDisc{}, errors.New("the game is a .gcz, whose header is compressed; convert it to .rvz in Dolphin")
	}
	if len(header) < at+0x20 {
		return DolphinDisc{}, errors.New("the file is too short to be a GameCube or Wii disc")
	}
	h := header[at:]
	d := DolphinDisc{ID: string(h[:6]), Disc: int(h[6])}
	switch {
	case binary.BigEndian.Uint32(h[0x18:]) == wiiMagic:
		d.Wii = true
	case binary.BigEndian.Uint32(h[0x1C:]) == gcMagic:
	default:
		return DolphinDisc{}, errors.New("the file isn't a GameCube or Wii disc image")
	}
	for _, c := range d.ID {
		if c < 0x20 || c > 0x7E {
			return DolphinDisc{}, errors.New("the disc's game ID isn't readable")
		}
	}
	return d, nil
}

// wiiDiscTitles is the title type of games played from disc, whose saves Dolphin keeps in the NAND
// under it.
const wiiDiscTitles = "00010000"

// WiiTitlesDir is the folder holding the save folders of Wii games played from disc, in the NAND at
// nand.
func WiiTitlesDir(nand string) string {
	return filepath.Join(nand, "title", wiiDiscTitles)
}

// WiiTitleDir is the folder a Wii disc's game keeps its save in, in the NAND at nand: named for the
// title code, its four characters in hex. Its data folder holds the save.
func WiiTitleDir(nand string, d DolphinDisc) string {
	return filepath.Join(WiiTitlesDir(nand), strings.ToLower(hex.EncodeToString([]byte(d.ID[:4]))))
}

// WiiSaveName is the name a Wii game's save goes by on RomM: its game ID, the same on every device.
func WiiSaveName(d DolphinDisc) string { return d.ID + ".zip" }
