// Package library holds helpers shared by the photo and music scanners.
package library

import (
	"crypto/sha1"
	"encoding/base32"
	"encoding/hex"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

var idEnc = base32.StdEncoding.WithPadding(base32.NoPadding)

// ID derives a stable 12-char id from a path relative to the media root.
// Stable ids keep statistics valid across rescans; renaming a file creates
// a new id.
func ID(rel string) string {
	sum := sha1.Sum([]byte(filepath.ToSlash(rel)))
	return strings.ToLower(idEnc.EncodeToString(sum[:]))[:12]
}

// Roots are the directories of one library (paths.photos / paths.music),
// loaded as a single library.
//
// Paths are stored relative to their root. Paths of the first root are
// stored as before (no prefix), so ids stay the same when more roots are
// added; paths of further roots get "@<key>/" in front, where key is derived
// from the root's absolute path — stable when roots are reordered, as long
// as the first one stays first.
type Roots []string

func rootKey(root string) string {
	sum := sha1.Sum([]byte(filepath.ToSlash(root)))
	return hex.EncodeToString(sum[:4])
}

// Prefix is put in front of relative paths of root i.
func (r Roots) Prefix(i int) string {
	if i == 0 {
		return ""
	}
	return "@" + rootKey(r[i]) + "/"
}

// Label is a short display name of root i (its directory name).
func (r Roots) Label(i int) string { return filepath.Base(r[i]) }

// Abs turns a stored relative path into an absolute one ("" without roots).
func (r Roots) Abs(rel string) string {
	if len(r) == 0 {
		return ""
	}
	if strings.HasPrefix(rel, "@") {
		if i := strings.IndexByte(rel, '/'); i > 1 {
			key := rel[1:i]
			for _, root := range r[1:] {
				if rootKey(root) == key {
					return filepath.Join(root, filepath.FromSlash(rel[i+1:]))
				}
			}
		}
	}
	return filepath.Join(r[0], filepath.FromSlash(rel))
}

// NaturalKey returns a sort key where digit runs are zero-padded so that
// "IMG_2" sorts before "IMG_10".
func NaturalKey(name string) string {
	var b strings.Builder
	runes := []rune(strings.ToLower(name))
	for i := 0; i < len(runes); {
		if unicode.IsDigit(runes[i]) {
			j := i
			for j < len(runes) && unicode.IsDigit(runes[j]) {
				j++
			}
			num := strings.TrimLeft(string(runes[i:j]), "0")
			if len(num) < 12 {
				b.WriteString(strings.Repeat("0", 12-len(num)))
			}
			b.WriteString(num)
			i = j
			continue
		}
		b.WriteRune(runes[i])
		i++
	}
	return b.String()
}

// ExtOf returns the lower-case extension without dot.
func ExtOf(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
}

// HasExt reports whether name has one of exts (lower-case, no dot).
func HasExt(name string, exts []string) bool {
	e := ExtOf(name)
	for _, x := range exts {
		if x == e {
			return true
		}
	}
	return false
}

// Hidden reports dot-files and macOS AppleDouble files ("._x.jpg").
func Hidden(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@eaDir") ||
		strings.EqualFold(name, "Thumbs.db")
}

// DecodeText converts file bytes to a UTF-8 string, stripping a BOM and
// falling back to GBK (common for Chinese .lrc / .txt files made on Windows).
func DecodeText(b []byte) string {
	b = []byte(strings.TrimPrefix(string(b), "\uFEFF"))
	if utf8.Valid(b) {
		return string(b)
	}
	if out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(b); err == nil {
		return string(out)
	}
	return strings.ToValidUTF8(string(b), "")
}
