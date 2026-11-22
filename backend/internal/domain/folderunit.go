package domain

import (
	"archive/zip"
	"bytes"
	"errors"
	"maps"
	"path"
	"slices"
	"strings"
	"time"
)

// A save kept as a folder of files, as the 3DS and Wii keep theirs, goes to RomM as one zip of the
// folder: each file under its path in the folder, with forward slashes.

// FolderUnit zips files, each under its path in the folder. Members are stored, not compressed, in
// name order at a fixed time, so the same files always give the same bytes.
func FolderUnit(files map[string][]byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			panic(err) // writing to a bytes.Buffer doesn't fail
		}
		_, _ = w.Write(files[name])
	}
	_ = zw.Close()
	return buf.Bytes()
}

// errFolderUnit is a save from RomM that isn't a zip of a save folder.
var errFolderUnit = errors.New("RomM's save isn't a zip of a save folder")

// maxFolderUnit bounds a save folder as read from RomM: the largest 3DS saves are a few MB.
const maxFolderUnit = 256 << 20

// ParseFolderUnit reads a zip of a save folder into its files by their path in the folder. A member
// whose path would leave the folder is refused, and folders are skipped.
func ParseFolderUnit(data []byte) (map[string][]byte, error) {
	if !isZip(data) {
		return nil, errFolderUnit
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errFolderUnit
	}
	out := map[string][]byte{}
	total := 0
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		name, ok := FolderMember(f.Name)
		if !ok {
			return nil, errors.New("RomM's save has a file outside its folder: " + f.Name)
		}
		member, err := readZipFile(f)
		if err != nil {
			return nil, errFolderUnit
		}
		if total += len(member); total > maxFolderUnit {
			return nil, errors.New("RomM's save is too large to be a save folder")
		}
		out[name] = member
	}
	if len(out) == 0 {
		return nil, errors.New("RomM's save is an empty zip")
	}
	return out, nil
}

// FolderMember cleans a zip member's name into a path inside the folder, refusing one that leaves
// it.
func FolderMember(name string) (string, bool) {
	name = strings.ReplaceAll(name, `\`, "/")
	clean := path.Clean(name)
	if clean == "." || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}
