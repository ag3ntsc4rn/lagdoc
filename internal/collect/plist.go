package collect

import (
	"bytes"
	"encoding/binary"
	"html"
	"regexp"
	"strings"
	"unicode/utf16"
)

// plistString returns a top-level string value from an XML or binary
// property list, or "" if it isn't there. It reads only what lagdoc needs
// (an app's version), not property lists in general.
func plistString(data []byte, key string) string {
	if bytes.HasPrefix(data, []byte("bplist00")) {
		return bplistString(data, key)
	}
	re := regexp.MustCompile(`<key>` + regexp.QuoteMeta(key) + `</key>\s*<string>([^<]*)</string>`)
	m := re.FindSubmatch(data)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(string(m[1])))
}

// bplistString reads a binary plist (format "bplist00"): a trailer at the
// end locates an offset table, which locates each object; the top object
// is a dict of key and value object references.
func bplistString(data []byte, key string) string {
	if len(data) < 8+32 {
		return ""
	}
	t := data[len(data)-32:]
	offSize, refSize := int(t[6]), int(t[7])
	numObjs := binary.BigEndian.Uint64(t[8:16])
	top := binary.BigEndian.Uint64(t[16:24])
	table := binary.BigEndian.Uint64(t[24:32])
	end := uint64(len(data) - 32)
	if offSize < 1 || offSize > 8 || refSize < 1 || refSize > 8 ||
		table > end || numObjs > (end-table)/uint64(offSize) || top >= numObjs {
		return ""
	}
	be := func(b []byte) uint64 {
		var v uint64
		for _, c := range b {
			v = v<<8 | uint64(c)
		}
		return v
	}
	// object returns an object's marker and the position after it.
	object := func(ref uint64) (byte, uint64, bool) {
		if ref >= numObjs {
			return 0, 0, false
		}
		p := table + ref*uint64(offSize)
		off := be(data[p : p+uint64(offSize)])
		if off >= end {
			return 0, 0, false
		}
		return data[off], off + 1, true
	}
	// length decodes an object's length: the marker's low nibble, or for
	// 0xF an int object that follows.
	length := func(marker byte, p uint64) (uint64, uint64, bool) {
		if n := marker & 0x0F; n != 0x0F {
			return uint64(n), p, true
		}
		if p >= end || data[p]>>4 != 0x1 {
			return 0, 0, false
		}
		size := uint64(1) << (data[p] & 0x0F)
		if size > 8 || p+1+size > end {
			return 0, 0, false
		}
		return be(data[p+1 : p+1+size]), p + 1 + size, true
	}
	str := func(ref uint64) (string, bool) {
		marker, p, ok := object(ref)
		if !ok {
			return "", false
		}
		n, p, ok := length(marker, p)
		if !ok {
			return "", false
		}
		switch marker >> 4 {
		case 0x5: // ASCII
			if n > end-p {
				return "", false
			}
			return string(data[p : p+n]), true
		case 0x6: // UTF-16BE, n code units
			if n > (end-p)/2 {
				return "", false
			}
			u := make([]uint16, n)
			for i := range u {
				u[i] = binary.BigEndian.Uint16(data[p+uint64(i)*2:])
			}
			return string(utf16.Decode(u)), true
		}
		return "", false
	}

	marker, p, ok := object(top)
	if !ok || marker>>4 != 0xD {
		return ""
	}
	n, p, ok := length(marker, p)
	if !ok || n > (end-p)/uint64(refSize)/2 {
		return ""
	}
	ref := func(i uint64) uint64 {
		q := p + i*uint64(refSize)
		return be(data[q : q+uint64(refSize)])
	}
	for i := range n {
		if k, ok := str(ref(i)); ok && k == key {
			v, _ := str(ref(n + i))
			return strings.TrimSpace(v)
		}
	}
	return ""
}
