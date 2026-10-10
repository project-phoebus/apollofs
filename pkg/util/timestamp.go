package util

import "time"

// The Apollo clock is a 48-bit count of 4-microsecond ticks since midnight
// 1980-01-01 UTC.  Label and block-header times are its high 32 bits
// ("clockh", 2^16 ticks = 262.144 ms); VTOCE times are the 48-bit value
// stored as clockh followed by the low 16 bits and a pad word.
var apolloEpoch = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

const tick = 4 * time.Microsecond

// FormatTimestamp formats a clockh value.
func FormatTimestamp(clockh uint32) string {
	return ClockhToTime(clockh).Format(time.RFC1123)
}

// ClockhToTime converts a clockh value to a time.
func ClockhToTime(clockh uint32) time.Time {
	return apolloEpoch.Add(time.Duration(uint64(clockh)<<16) * tick)
}

// Clock returns the 48-bit Apollo clock for t.
func Clock(t time.Time) uint64 {
	return uint64(t.Sub(apolloEpoch) / tick)
}

// TimestampToApolloEpoch returns clockh for t.
func TimestampToApolloEpoch(t time.Time) uint32 {
	return uint32(Clock(t) >> 16)
}

// ClockBytes returns the 8-byte VTOCE time image (clockh, low 16 bits, 0).
func ClockBytes(t time.Time) [8]byte {
	c := Clock(t)
	var b [8]byte
	b[0], b[1], b[2], b[3] = byte(c>>40), byte(c>>32), byte(c>>24), byte(c>>16)
	b[4], b[5] = byte(c>>8), byte(c)
	return b
}
