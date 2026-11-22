package domain

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// A Wii U game Cemu plays is a .wua, Cemu's ZArchive of one or more titles (the game, its update,
// its DLC), or a folder of the game's code, content and meta. The game's own title, of type
// 00050000, names its save folder in the MLC: usr/save/00050000/<title's low half>.

// wiiuGameType is the title type, the high half of the title ID, of a Wii U game itself.
const wiiuGameType = 0x00050000

const (
	zarchiveMagic      = 0x169f52d6
	zarchiveFooterSize = 144
	// maxZArchiveIndex bounds the name table and file tree a .wua is read for; a game's are tens of
	// KB.
	maxZArchiveIndex = 16 << 20
)

var wuaTitleFolder = regexp.MustCompile(`^([0-9a-fA-F]{16})_v\d+$`)

// WUATitleID reads the title ID of the game in a .wua of size bytes: the root folder named for a
// title of type 00050000, as Cemu names each title it packs, <title ID>_v<version>. Only the
// archive's footer, name table and file tree are read, which ZArchive keeps uncompressed.
func WUATitleID(r io.ReaderAt, size int64) (uint64, error) {
	if size < zarchiveFooterSize {
		return 0, errors.New("the file is too short to be a .wua")
	}
	footer := make([]byte, zarchiveFooterSize)
	if _, err := r.ReadAt(footer, size-zarchiveFooterSize); err != nil {
		return 0, err
	}
	if binary.BigEndian.Uint32(footer[140:]) != zarchiveMagic {
		return 0, errors.New("the file isn't a .wua (ZArchive)")
	}
	section := func(i int) ([]byte, error) {
		off, n := binary.BigEndian.Uint64(footer[i*16:]), binary.BigEndian.Uint64(footer[i*16+8:])
		if n > maxZArchiveIndex || off > uint64(size) || n > uint64(size)-off {
			return nil, errors.New("the .wua's index is damaged")
		}
		b := make([]byte, n)
		_, err := r.ReadAt(b, int64(off))
		return b, err
	}
	names, err := section(2)
	if err != nil {
		return 0, err
	}
	tree, err := section(3)
	if err != nil {
		return 0, err
	}
	name := func(off uint32) (string, bool) {
		if int(off) >= len(names) {
			return "", false
		}
		n, at := int(names[off]), int(off)+1
		if n&0x80 != 0 {
			if at >= len(names) {
				return "", false
			}
			n, at = (n&0x7F)<<8|int(names[at]), at+1
		}
		if at+n > len(names) {
			return "", false
		}
		return string(names[at : at+n]), true
	}
	if len(tree) < 16 {
		return 0, errors.New("the .wua holds nothing")
	}
	// The root is the tree's first entry, a folder whose children run from start for count.
	start, count := binary.BigEndian.Uint32(tree[4:]), binary.BigEndian.Uint32(tree[8:])
	var seen []string
	for i := uint64(start); i < uint64(start)+uint64(count) && (i+1)*16 <= uint64(len(tree)); i++ {
		e := tree[i*16:]
		head := binary.BigEndian.Uint32(e)
		if head>>31 == 1 {
			continue // a file at the root isn't a title
		}
		n, ok := name(head & 0x7FFFFFFF)
		if !ok {
			continue
		}
		seen = append(seen, n)
		if m := wuaTitleFolder.FindStringSubmatch(n); m != nil {
			id, _ := strconv.ParseUint(m[1], 16, 64)
			if id>>32 == wiiuGameType {
				return id, nil
			}
		}
	}
	return 0, fmt.Errorf("the .wua holds no game title, only %s", strings.Join(seen, ", "))
}

var metaTitleID = regexp.MustCompile(`<title_id[^>]*>\s*([0-9a-fA-F]{16})\s*</title_id>`)

// WiiUMetaTitleID reads the title ID from a Wii U game's meta/meta.xml or code/app.xml.
func WiiUMetaTitleID(xml []byte) (uint64, error) {
	m := metaTitleID.FindSubmatch(xml)
	if m == nil {
		return 0, errors.New("it names no title ID")
	}
	return strconv.ParseUint(string(m[1]), 16, 64)
}

// WiiUTitleDir is the folder Cemu keeps the save of the Wii U title in, under the MLC's usr/save
// folder at saves: its user folder, one per account and common, and its meta.
func WiiUTitleDir(saves string, titleID uint64) string {
	return filepath.Join(saves, fmt.Sprintf("%08x", titleID>>32), fmt.Sprintf("%08x", titleID&0xFFFFFFFF))
}

// WiiUGamesDir is the folder holding every Wii U game's save folder under the MLC's usr/save.
func WiiUGamesDir(saves string) string {
	return filepath.Join(saves, fmt.Sprintf("%08x", wiiuGameType))
}

// WiiUSaveName is the name a Wii U game's save goes by on RomM: its title ID.
func WiiUSaveName(titleID uint64) string { return fmt.Sprintf("%016X.zip", titleID) }
