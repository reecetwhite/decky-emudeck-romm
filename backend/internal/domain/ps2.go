package domain

import "strings"

// PS2Serial is the serial of the PlayStation 2 disc whose SYSTEM.CNF is cnf: from the executable
// its BOOT2 line boots, SCUS_971.24 giving SCUS-97124. Empty when it names none.
func PS2Serial(cnf []byte) string {
	for line := range strings.FieldsFuncSeq(string(cnf), func(r rune) bool { return r == '\r' || r == '\n' }) {
		line = strings.Map(func(r rune) rune {
			if r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, line)
		key, value, _ := strings.Cut(line, "=")
		if !strings.EqualFold(key, "boot2") {
			continue
		}
		if len(value) >= 7 && strings.EqualFold(value[:7], "cdrom0:") {
			value = value[7:]
		}
		value = strings.TrimLeft(value, `/\`)
		if i := strings.LastIndexByte(value, ';'); i >= 0 {
			value = value[:i]
		}
		if i := strings.LastIndexAny(value, `/\`); i >= 0 {
			value = value[i+1:]
		}
		value = strings.ReplaceAll(value, ".", "")
		serial := strings.ToUpper(strings.ReplaceAll(value, "_", "-"))
		// A serial is four letters, a dash and five digits.
		if len(serial) != 10 || serial[4] != '-' {
			return ""
		}
		return serial
	}
	return ""
}

// PS2SaveOf says whether the memory card folder named name holds a save of the game with serial.
// PS2 games name their save folders for their region and serial, BASCUS-97124 and a suffix of the
// game's own.
func PS2SaveOf(name, serial string) bool {
	return len(name) >= 2+len(serial) && strings.EqualFold(name[2:2+len(serial)], serial)
}

// PS2SaveName is the name a PS2 game's saves go by on RomM: its serial.
func PS2SaveName(serial string) string { return serial + ".zip" }
