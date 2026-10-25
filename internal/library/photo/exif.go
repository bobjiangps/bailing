package photo

import (
	"strconv"
	"strings"
	"time"
)

// Exif is the trimmed EXIF info shown in the viewer panel.
type Exif struct {
	Make     string  `json:"make,omitempty"`
	Model    string  `json:"model,omitempty"`
	Lens     string  `json:"lens,omitempty"`
	Focal    string  `json:"focal,omitempty"`
	FNumber  string  `json:"fnumber,omitempty"`
	Exposure string  `json:"exposure,omitempty"`
	ISO      string  `json:"iso,omitempty"`
	Taken    string  `json:"taken,omitempty"`
	Width    int     `json:"width,omitempty"`
	Height   int     `json:"height,omitempty"`
	Lat      float64 `json:"lat,omitempty"`
	Lon      float64 `json:"lon,omitempty"`
}

// human extracts libexif's formatted value from a vipsheader field:
// "9/5 (f/1.8, Rational, 1 components, 8 bytes)" -> "f/1.8"
func human(v string) string {
	i := strings.Index(v, " (")
	if i < 0 || !strings.HasSuffix(v, ")") {
		return strings.TrimSpace(v)
	}
	inner := v[i+2 : len(v)-1]
	parts := strings.Split(inner, ", ")
	if len(parts) > 3 {
		parts = parts[:len(parts)-3] // drop "<type>, N components, N bytes"
	} else {
		parts = parts[:1]
	}
	return strings.TrimSpace(strings.Join(parts, ", "))
}

func first(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if h := human(v); h != "" {
				return h
			}
		}
	}
	return ""
}

// parseHeader converts `vipsheader -a` output into Exif + orientation + time.
func parseHeader(m map[string]string) (ex Exif, orientation int, taken time.Time) {
	ex.Width, _ = strconv.Atoi(m["width"])
	ex.Height, _ = strconv.Atoi(m["height"])
	ex.Make = first(m, "exif-ifd0-Make")
	ex.Model = first(m, "exif-ifd0-Model")
	ex.Lens = first(m, "exif-ifd2-LensModel")
	ex.Focal = first(m, "exif-ifd2-FocalLength")
	ex.FNumber = first(m, "exif-ifd2-FNumber")
	ex.Exposure = first(m, "exif-ifd2-ExposureTime")
	ex.ISO = first(m, "exif-ifd2-ISOSpeedRatings", "exif-ifd2-PhotographicSensitivity")
	if ex.Model != "" && ex.Make != "" && strings.HasPrefix(ex.Model, ex.Make) {
		ex.Make = "" // "Apple iPhone 15" is enough
	}

	if o := m["orientation"]; o != "" {
		orientation, _ = strconv.Atoi(strings.Fields(o)[0])
	} else if o := m["exif-ifd0-Orientation"]; o != "" {
		orientation, _ = strconv.Atoi(strings.Fields(o)[0])
	}

	dt := first(m, "exif-ifd2-DateTimeOriginal", "exif-ifd0-DateTime")
	if t, err := time.ParseInLocation("2006:01:02 15:04:05", dt, time.Local); err == nil {
		taken = t
		ex.Taken = t.Format("2006-01-02 15:04")
	}

	lat := parseDMS(first(m, "exif-ifd3-GPSLatitude"))
	lon := parseDMS(first(m, "exif-ifd3-GPSLongitude"))
	if lat != 0 || lon != 0 {
		if strings.HasPrefix(first(m, "exif-ifd3-GPSLatitudeRef"), "S") {
			lat = -lat
		}
		if strings.HasPrefix(first(m, "exif-ifd3-GPSLongitudeRef"), "W") {
			lon = -lon
		}
		ex.Lat, ex.Lon = lat, lon
	}
	return
}

// parseDMS parses "31, 14, 22.71" (degrees, minutes, seconds).
func parseDMS(s string) float64 {
	if s == "" {
		return 0
	}
	parts := strings.Split(s, ",")
	var vals [3]float64
	for i := 0; i < len(parts) && i < 3; i++ {
		vals[i], _ = strconv.ParseFloat(strings.TrimSpace(parts[i]), 64)
	}
	return vals[0] + vals[1]/60 + vals[2]/3600
}
