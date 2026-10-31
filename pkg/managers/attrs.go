package managers

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/project-phoebus/apollofs/pkg/fs"
	"github.com/project-phoebus/apollofs/pkg/uid"
)

// Attrs is the 0x78-byte VTOCE attribute image an rbak tape stores for each
// object (VTOCE bytes 0x00..0x77): kind/sub type/flags, type UID, length,
// the four clocks, owner/group/org UIDs, rights and ACL flags, link count.
type Attrs [fs.VTOCEAttrSize]byte

// ParseAttrs decodes a hex attribute image (whitespace ignored).
func ParseAttrs(s string) (Attrs, error) {
	var a Attrs
	s = strings.Join(strings.Fields(s), "")
	b, err := hex.DecodeString(s)
	if err != nil {
		return a, err
	}
	if len(b) != len(a) {
		return a, fmt.Errorf("attribute image is %d bytes, want %d", len(b), len(a))
	}
	copy(a[:], b)
	return a, nil
}

func (a Attrs) String() string { return hex.EncodeToString(a[:]) }

func mustAttrs(s string) Attrs {
	a, err := ParseAttrs(s)
	if err != nil {
		panic(err)
	}
	return a
}

// The protection image SR10.2's own tapes carry on nearly every object:
// owner 00800000, group 00800040, org 00800080, rights 1f/1f/1f/0f.
const stdProt = "00800000 00000000 00800040 00000000 00800080 00000000 1f1f1f0f 00000000 0000000c 0000000c 0000000c"

// DefaultFileAttrs / DefaultDirAttrs are what an object gets when no
// attribute image is supplied (the SR10.2 boot tape's /bscom/rbak_shell and
// /bscom headers with the UIDs, sizes and clocks cleared).
var (
	DefaultFileAttrs = mustAttrs("01009000 0000000000000000 0000000000000000 00000000 00000000" +
		strings.Repeat("0", 4*8*2) + "0000000000000000 00000000 " + stdProt + " 00010000")
	DefaultDirAttrs = mustAttrs("01019000 0000000000000000 0000000000000000 00000000 00000000" +
		strings.Repeat("0", 4*8*2) + "0000000000000000 00000000 " + stdProt + " 00010000")
)

// DefaultDefProt is a directory's default protection for new entries as the
// SR10.2 tapes record it.
var DefaultDefProt = func() fs.DirDefProt {
	var p fs.DirDefProt
	b, _ := hex.DecodeString(strings.Join(strings.Fields(stdProt), ""))
	copy(p.Prot[:], b)
	return p
}()

// rootDefProt is the default-protection slot of the "/" and "//" pages an
// SR10.3 invol wrote: no owner, rights 0f/0f/10/0f, extension all ones.
var rootDefProt = func() fs.DirDefProt {
	var p fs.DirDefProt
	b, _ := hex.DecodeString("000000000000000000000000000000000000000000000000" + "0f0f100f" + "00000000" + "ffffffffffffffffffffffff")
	copy(p.Prot[:], b)
	return p
}()

// rootDirAttrs is the "/" VTOCE header of an SR10.3 volume (owner 0x542,
// group 00800049, org 00800080, rights 0f/05/10/05, ACL flags 0x00400000).
var rootDirAttrs = mustAttrs("01029000 0000000000000000 0000000000000000 00000400 00000001" +
	strings.Repeat("0", 4*8*2) + "0000000000000000 00000000" +
	" 00000542 00000000 00800049 00000000 00800080 00000000 0f05 1005 00400000 00000000 00000000 0000000c 00010000")

// pagingFileAttrs is the OS paging file VTOCE header of an SR10.3 volume:
// type os_pg_file (0x323), not catalogued, rights 0f/0f/10/0f.
var pagingFileAttrs = mustAttrs("01009000 0000000000000000 0000032300000000 00000000 00000000" +
	strings.Repeat("0", 4*8*2) + "0000000000000000 00000000" +
	" 00000542 00000000 00800049 00000000 00800080 00000000 0f0f 100f 00400000 00000000 00000000 0000000c 00010000")

// ParseDefProt decodes a 0x34-byte (or 0x2C-byte, ACL UID omitted)
// default-protection slot.
func ParseDefProt(s string) (fs.DirDefProt, error) {
	var p fs.DirDefProt
	b, err := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	if err != nil {
		return p, err
	}
	if len(b) != 44 && len(b) != 52 {
		return p, fmt.Errorf("default protection is %d bytes, want 44 or 52", len(b))
	}
	copy(p.Prot[:], b)
	if len(b) == 52 {
		p.ACLUID = uid.UID{Hi: be32(b[44:]), Lo: be32(b[48:])}
	}
	return p, nil
}

func be32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// TypeUIDs are the canned type UIDs (high word; low word 0) by the names
// SR10 tools print.
var TypeUIDs = map[string]uint32{
	"rec": 0x300, "hdru": 0x301, "obj": 0x302, "undef": 0x304, "pad": 0x305,
	"ipad": 0x309, "sio": 0x30a, "ddf": 0x30b, "mbx": 0x30c, "null": 0x30d,
	"d3m_area": 0x30e, "d3m_sch": 0x30f, "pipe": 0x310, "uasc": 0x311,
	"dir": 0x312, "unix_dir": 0x313, "mt": 0x314, "sysboot": 0x315,
	"case_hm": 0x316, "bitmap": 0x317, "objlib": 0x318, "lheap": 0x319,
	"dm_edit": 0x31a, "tcp": 0x31b, "pty": 0x31c, "pty_slave": 0x31d,
	"slink": 0x31e, "ipcsock": 0x31f, "vte": 0x320, "unstruct": 0x321,
	"coff": 0x322, "os_pg_file": 0x323, "dev_tty": 0x324, "cmpexe": 0x325,
	"osio": 0x326, "nil": 0,
}

// TypeName names a type UID.
func TypeName(u uid.UID) string {
	if u.Lo == 0 {
		for n, v := range TypeUIDs {
			if v == u.Hi {
				return n
			}
		}
	}
	return fmt.Sprintf("%08x.%08x", u.Hi, u.Lo)
}

// ParseTypeUID accepts a type name or "hhhhhhhh.llllllll".
func ParseTypeUID(s string) (uid.UID, error) {
	if v, ok := TypeUIDs[s]; ok {
		return uid.UID{Hi: v}, nil
	}
	var u uid.UID
	if _, err := fmt.Sscanf(s, "%x.%x", &u.Hi, &u.Lo); err != nil {
		return u, fmt.Errorf("unknown type %q", s)
	}
	return u, nil
}
