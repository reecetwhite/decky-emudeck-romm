package domain

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
)

// CTRHeaderSize is how much of a 3DS game file ParseCTRTitleID reads: its NCSD or NCCH header.
const CTRHeaderSize = 0x200

// ParseCTRTitleID reads the title ID from the start of a 3DS game file. A cartridge image (.3ds,
// .cci) is an NCSD, whose media ID is the title ID; a lone executable partition (.cxi, .app) is an
// NCCH, which carries its program ID. Both headers sit unencrypted at 0x100, behind a signature.
func ParseCTRTitleID(header []byte) (uint64, error) {
	if len(header) < CTRHeaderSize {
		return 0, errors.New("the file is too short to be a 3DS game")
	}
	switch string(header[0x100:0x104]) {
	case "NCSD":
		return binary.LittleEndian.Uint64(header[0x108:]), nil
	case "NCCH":
		return binary.LittleEndian.Uint64(header[0x118:]), nil
	}
	return 0, errors.New("the file isn't a 3DS cartridge image (.3ds or .cci) or executable (.cxi)")
}

// ctrZeroID is the ID0 and ID1 folder name Azahar's virtual SD card uses: it has no console keys to
// hash, so both are zeros.
const ctrZeroID = "00000000000000000000000000000000"

// CTRTitleDir is the folder an emulated 3DS keeps a title's save in under its SD card, sdmc: the
// title's data folder holds the save archive, data/00000001/, and its metadata,
// data/00000001.metadata.
func CTRTitleDir(sdmc string, titleID uint64) string {
	return filepath.Join(sdmc, "Nintendo 3DS", ctrZeroID, ctrZeroID, "title",
		fmt.Sprintf("%08x", titleID>>32), fmt.Sprintf("%08x", titleID&0xFFFFFFFF))
}

// CTRTitlesDir is the folder holding every title's folder under the SD card at sdmc, which the save
// watch covers in full.
func CTRTitlesDir(sdmc string) string {
	return filepath.Join(sdmc, "Nintendo 3DS", ctrZeroID, ctrZeroID, "title")
}

// CTRSaveName is the name a 3DS game's save goes by on RomM: its title ID, which names it the same
// on every device whatever the game's file is called.
func CTRSaveName(titleID uint64) string {
	return fmt.Sprintf("%016X.zip", titleID)
}
