package music

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
)

// Duration estimates the playback length of an audio file in milliseconds
// by reading container headers only (no decoding). Returns 0 when unknown;
// the browser reports the real duration once the track is loaded.
func Duration(path, ext string) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0
	}
	switch ext {
	case "mp3":
		return mp3Duration(f, st.Size())
	case "flac":
		return flacDuration(f)
	case "m4a", "mp4", "aac", "alac":
		return mp4Duration(f, st.Size())
	case "wav":
		return wavDuration(f)
	case "ogg", "opus", "oga":
		return oggDuration(f, st.Size())
	}
	return 0
}

// skipID3v2 returns the offset after an ID3v2 tag (0 if none).
func skipID3v2(r io.ReaderAt) int64 {
	hdr := make([]byte, 10)
	if _, err := r.ReadAt(hdr, 0); err != nil || string(hdr[:3]) != "ID3" {
		return 0
	}
	size := int64(hdr[6]&0x7f)<<21 | int64(hdr[7]&0x7f)<<14 | int64(hdr[8]&0x7f)<<7 | int64(hdr[9]&0x7f)
	off := 10 + size
	if hdr[5]&0x10 != 0 { // footer present
		off += 10
	}
	return off
}

var (
	mp3Bitrates = [2][16]int{
		{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}, // MPEG1 L3
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},     // MPEG2/2.5 L3
	}
	mp3Rates = [3][3]int{{44100, 48000, 32000}, {22050, 24000, 16000}, {11025, 12000, 8000}}
)

func mp3Duration(f *os.File, size int64) int64 {
	start := skipID3v2(f)
	buf := make([]byte, 64*1024)
	n, _ := f.ReadAt(buf, start)
	buf = buf[:n]
	// find the first valid frame header
	for i := 0; i+4 <= len(buf); i++ {
		if buf[i] != 0xFF || buf[i+1]&0xE0 != 0xE0 {
			continue
		}
		verBits := (buf[i+1] >> 3) & 3 // 3=MPEG1 2=MPEG2 0=MPEG2.5
		layer := (buf[i+1] >> 1) & 3   // 1 = Layer III
		brIdx := buf[i+2] >> 4
		srIdx := (buf[i+2] >> 2) & 3
		if verBits == 1 || layer != 1 || brIdx == 0 || brIdx == 15 || srIdx == 3 {
			continue
		}
		mpeg1 := verBits == 3
		verRow := 0
		switch verBits {
		case 2:
			verRow = 1
		case 0:
			verRow = 2
		}
		rate := mp3Rates[verRow][srIdx]
		tbl := 0
		if !mpeg1 {
			tbl = 1
		}
		bitrate := mp3Bitrates[tbl][brIdx] * 1000
		mono := buf[i+3]>>6 == 3
		spf := 1152
		if !mpeg1 {
			spf = 576
		}
		// Xing / Info header offset depends on version and channel mode
		xoff := 4 + 32
		switch {
		case mpeg1 && mono:
			xoff = 4 + 17
		case !mpeg1 && !mono:
			xoff = 4 + 17
		case !mpeg1 && mono:
			xoff = 4 + 9
		}
		if i+xoff+12 <= len(buf) {
			tag := string(buf[i+xoff : i+xoff+4])
			if tag == "Xing" || tag == "Info" {
				flags := binary.BigEndian.Uint32(buf[i+xoff+4:])
				if flags&1 != 0 {
					frames := binary.BigEndian.Uint32(buf[i+xoff+8:])
					return int64(frames) * int64(spf) * 1000 / int64(rate)
				}
			}
		}
		if i+4+32+18 <= len(buf) && string(buf[i+4+32:i+4+36]) == "VBRI" {
			frames := binary.BigEndian.Uint32(buf[i+4+32+14:])
			return int64(frames) * int64(spf) * 1000 / int64(rate)
		}
		// constant bitrate estimate
		audio := size - start - int64(i)
		if bitrate > 0 && audio > 0 {
			return audio * 8 * 1000 / int64(bitrate)
		}
		return 0
	}
	return 0
}

func flacDuration(f *os.File) int64 {
	off := skipID3v2(f)
	b := make([]byte, 4+4+34)
	if _, err := f.ReadAt(b, off); err != nil || string(b[:4]) != "fLaC" {
		return 0
	}
	si := b[8:] // STREAMINFO body (first metadata block is always STREAMINFO)
	rate := int64(si[10])<<12 | int64(si[11])<<4 | int64(si[12])>>4
	total := int64(si[13]&0x0F)<<32 | int64(si[14])<<24 | int64(si[15])<<16 | int64(si[16])<<8 | int64(si[17])
	if rate == 0 {
		return 0
	}
	return total * 1000 / rate
}

// mp4Duration walks top-level atoms to moov/mvhd.
func mp4Duration(f *os.File, size int64) int64 {
	var find func(start, end int64, path []string) int64
	find = func(start, end int64, path []string) int64 {
		hdr := make([]byte, 16)
		for pos := start; pos+8 <= end; {
			if _, err := f.ReadAt(hdr[:8], pos); err != nil {
				return 0
			}
			sz := int64(binary.BigEndian.Uint32(hdr[:4]))
			typ := string(hdr[4:8])
			hlen := int64(8)
			if sz == 1 {
				if _, err := f.ReadAt(hdr[8:16], pos+8); err != nil {
					return 0
				}
				sz = int64(binary.BigEndian.Uint64(hdr[8:16]))
				hlen = 16
			} else if sz == 0 {
				sz = end - pos
			}
			if sz < hlen {
				return 0
			}
			if typ == path[0] {
				if len(path) == 1 {
					body := make([]byte, 32)
					if _, err := f.ReadAt(body, pos+hlen); err != nil && err != io.EOF {
						return 0
					}
					var scale, dur uint64
					if body[0] == 1 {
						scale = uint64(binary.BigEndian.Uint32(body[20:24]))
						dur = binary.BigEndian.Uint64(body[24:32])
					} else {
						scale = uint64(binary.BigEndian.Uint32(body[12:16]))
						dur = uint64(binary.BigEndian.Uint32(body[16:20]))
					}
					if scale == 0 {
						return 0
					}
					return int64(dur * 1000 / scale)
				}
				return find(pos+hlen, pos+sz, path[1:])
			}
			pos += sz
		}
		return 0
	}
	return find(0, size, []string{"moov", "mvhd"})
}

func wavDuration(f *os.File) int64 {
	hdr := make([]byte, 12)
	if _, err := f.ReadAt(hdr, 0); err != nil || string(hdr[:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" {
		return 0
	}
	var byteRate, dataSize int64
	pos := int64(12)
	ch := make([]byte, 8)
	for i := 0; i < 64; i++ {
		if _, err := f.ReadAt(ch, pos); err != nil {
			break
		}
		id := string(ch[:4])
		sz := int64(binary.LittleEndian.Uint32(ch[4:8]))
		switch id {
		case "fmt ":
			fb := make([]byte, 16)
			if _, err := f.ReadAt(fb, pos+8); err == nil {
				byteRate = int64(binary.LittleEndian.Uint32(fb[8:12]))
			}
		case "data":
			dataSize = sz
		}
		if byteRate > 0 && dataSize > 0 {
			return dataSize * 1000 / byteRate
		}
		pos += 8 + sz + sz%2
	}
	return 0
}

func oggDuration(f *os.File, size int64) int64 {
	head := make([]byte, 128)
	if _, err := f.ReadAt(head, 0); err != nil && err != io.EOF {
		return 0
	}
	var rate int64
	preSkip := int64(0)
	if i := bytes.Index(head, []byte("\x01vorbis")); i >= 0 && i+16 <= len(head) {
		rate = int64(binary.LittleEndian.Uint32(head[i+12 : i+16]))
	} else if i := bytes.Index(head, []byte("OpusHead")); i >= 0 && i+12 <= len(head) {
		rate = 48000
		preSkip = int64(binary.LittleEndian.Uint16(head[i+10 : i+12]))
	}
	if rate == 0 {
		return 0
	}
	tailLen := int64(65536)
	if tailLen > size {
		tailLen = size
	}
	tail := make([]byte, tailLen)
	if _, err := f.ReadAt(tail, size-tailLen); err != nil && err != io.EOF {
		return 0
	}
	i := bytes.LastIndex(tail, []byte("OggS"))
	if i < 0 || i+14 > len(tail) {
		return 0
	}
	granule := int64(binary.LittleEndian.Uint64(tail[i+6 : i+14]))
	if granule <= preSkip {
		return 0
	}
	return (granule - preSkip) * 1000 / rate
}
