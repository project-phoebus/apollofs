package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClock(t *testing.T) {
	// 1 second after the epoch is 250000 ticks
	require.Equal(t, uint64(250000), Clock(time.Date(1980, 1, 1, 0, 0, 1, 0, time.UTC)))
	ts := time.Date(1995, 6, 1, 12, 0, 0, 0, time.UTC)
	h := TimestampToApolloEpoch(ts)
	require.WithinDuration(t, ts, ClockhToTime(h), 300*time.Millisecond)
	b := ClockBytes(ts)
	require.Equal(t, byte(h>>24), b[0])
	require.Equal(t, byte(0), b[6])
}
