package protocol

import "strings"

const geohashAlphabet = "0123456789bcdefghjkmnpqrstuvwxyz"

// Geohash encodes a coordinate at the given precision. Phones call it with
// precision 7 (about 150 m) so raw GPS never leaves the device.
func Geohash(lat, lon float64, precision int) string {
	latLo, latHi, lonLo, lonHi := -90.0, 90.0, -180.0, 180.0
	var sb strings.Builder
	bit, ch, even := 0, 0, true
	for sb.Len() < precision {
		if even {
			mid := (lonLo + lonHi) / 2
			if lon >= mid {
				ch |= 1 << (4 - bit)
				lonLo = mid
			} else {
				lonHi = mid
			}
		} else {
			mid := (latLo + latHi) / 2
			if lat >= mid {
				ch |= 1 << (4 - bit)
				latLo = mid
			} else {
				latHi = mid
			}
		}
		even = !even
		if bit < 4 {
			bit++
		} else {
			sb.WriteByte(geohashAlphabet[ch])
			bit, ch = 0, 0
		}
	}
	return sb.String()
}

func isGeohash(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune(geohashAlphabet, r) {
			return false
		}
	}
	return true
}
