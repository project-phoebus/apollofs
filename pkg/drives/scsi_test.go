package drives

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSCSIDisk(t *testing.T) {
	d, err := ParseSCSIDisk("400/16/63")
	require.NoError(t, err)
	require.Equal(t, int64(403200), d.Sectors())
	require.Equal(t, int64(206438400), d.ImageBytes())
	// last LBA 403199 x 512 / 1024
	require.Equal(t, 201599, d.Blocks())
	// 403199 x 512 = 206437888 bytes = 19 units of 10 MB
	require.Equal(t, int16(0x713), d.DType())
	require.Equal(t, 1008, d.BlocksPerCylinder())
	require.Equal(t, d, MAMEGuess(d.ImageBytes()))
	require.Equal(t, "SCSI, about 190 MB", Name(d.DType()))
	require.Equal(t, "PRIAM 7050", Name(0x105))

	// 2.6 GB and up: WIN_SCSI_$DINIT's class byte overflows to 0x700
	big := SCSIDisk{Cylinders: 6000, Heads: 16, SectorsPerTrack: 63}
	require.Equal(t, int16(0x700), big.DType())

	for _, s := range []string{"400/16", "0/16/63", "400/0/63", "400/16/63/1", "4000/255/255"} {
		_, err := ParseSCSIDisk(s)
		require.Error(t, err, s)
	}
}

func TestMAMEGuess(t *testing.T) {
	// a size no 63 x 16 geometry divides: MAME takes the largest
	// sectors-per-track that divides, then the most heads
	require.Equal(t, SCSIDisk{Cylinders: 1000, Heads: 15, SectorsPerTrack: 62}, MAMEGuess(1000*15*62*512))
}
