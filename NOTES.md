# apollofs notes: SR10 on-disk format, the SR10.2 DN300 and DN2500 volumes

## On-disk format (as written and checked by apollofs)

Sources: the kernel reconstruction in domainos-archeology/source/domain_os
(bat/, vtoc/, dir/, ast/, disk/, os/) and an SR10.3 volume an installer wrote.

- Blocks are 1056 bytes: a 32-byte header (object UID, page within object,
  clockh written, system-type word, physical daddr) and 1024 data bytes.
  A SCSI volume has no headers: 1024-byte blocks (see "SCSI volumes").
  Data pages: system-type 0 (files) or sub type << 16 (directory pages);
  file-map blocks: level << 24, page = first page mapped (32 for a level-1
  root, 288 for a level-2 root).  System blocks (labels, BAT, VTOC) use
  page = their own daddr.
- PV label (block 0) gains SR10 fields at 0xA0..0xC4 (addr_end, striping
  set, interleave, sectors_per_block = 1, invol's revision string; on a
  SCSI volume also 0xF6/0xF8, the badspot and diagnostic areas' sizes).
- LV label (LV block 0, 0x308 bytes, bat_$label_t): BAT header at 0x2C, the
  100-byte VTOC header (vtoc_$vol_t) at 0x4C, times at 0xB0.., salvage
  flag 0xCE, BAT partition table at 0xFC (64 x {free count, status byte +
  current VTOCE block}).  The alternate copy lives in the last block.
- BAT: a set bit is a FREE block; LSB-first in big-endian longwords; LV
  blocks 0..10 (label + sysboot) are outside it.
- VTOC: bucket blocks (4 x 20 {uid, vtocx} slots, magic 0xFEDCA985) found
  by hashing the UID (hash type 2); VTOCE blocks hold 3 x 0x150-byte
  VTOCEs (magic 0xFEDCA984); BAT partitions remember their current VTOCE
  block.  VTOCE bytes 0x00..0x77 are exactly what an rbak tape stores per
  object, so tape attributes are copied verbatim.
- Directories: B-tree pages (see pkg/fs/dirpage.go), "\0" sentinel, entries
  sorted by unsigned bytes; "//" catalogues "/" as node_<id> (hard-link
  entry); "/"'s parent is the canned root UID 0x308.
- The OS paging file: an uncatalogued os_pg_file object whose VTOCX is the
  VTOC header's current_vtoce (label +0x5C).  It must have disk blocks:
  OS_$INIT wires the pageable kernel into it and a page without a block
  is "The OS paging file is too small".  invol preallocates 1024 pages.

## SCSI volumes (invol --scsi)

Sources: the SR10.2 kernel's WIN_SCSI and DISK modules (domain_os/win_scsi,
disk/) and the SR10.2 SAU9 standalone invol (install/ri.apollo.os.v.10.2/
sau9/invol, loaded at 0x04002C00: file offset = address - 0x04002C00).

- No block headers.  WIN_SCSI_$CINIT registers unit flags 0xAA00; bit
  0x8000 turns off DISK_IO's header stamp on write and check on read, and
  WIN_SCSI_$DO_IO moves the 1024 data bytes of block daddr with READ(10)/
  WRITE(10) at LBA daddr x (1024 / sector length).  The image is a flat
  array of 1024-byte blocks; with 512-byte sectors Apollo block N is LBAs
  2N and 2N+1, image byte N x 1024.  Bit 0x200 = logically addressed: no
  CHS mapping, no striping.
- apollofs tells the layouts apart when it opens an image: the PV label's
  "APOLLO" is at byte 2 of a headerless image and at byte 34, behind the
  header, of one with headers.  Headerless blocks read back with a zero
  header; only their data is written, and copyout and check skip the
  header comparisons.
- The disk is given as cylinders/heads/sectors per track (512-byte
  sectors): what WIN_SCSI_$DINIT reads from the rigid disk geometry page
  (4), the format device page (3) and READ CAPACITY.  The image is the
  whole disk, C x H x S x 512 bytes.
- The PV label is what the SR10.2 invol writes for such a disk:
  - drive type: DINIT's, 0x700 plus (last LBA x sector length) / 10 MB in
    the low byte (0x700 if that passes 0x7FF);
  - N = last LBA x sector length / 1024: DINIT's num_blocks for an assign,
    from the last LBA, not the count (one block short);
  - blocks_per_track = sectors per track, tracks_per_cyl = heads (DINIT's
    pages, unconverted: "logically addressed, geometry values may not
    reflect actual physical geometry");
  - sectors_per_block = 1 (also at 0x9E): invol sets the pvlabel_info word
    at +6 to 1 before DISK_$PV_ASSIGN_N (0x4013B66), DINIT writes only the
    first four bytes (0, the format page's sector length), and the assign
    takes sector_size_code = log2[that word] (0x40232CC); invol copies it
    into its descriptor (0x4013CEC) and the label (0x401DA04);
  - sector_start 0, sector_size 512, precomp 0, word 0xC0 0 (the same
    four-word record: 0x4013CCE..0x4013CF6);
  - the reserved areas (invol's sizing for class 0x700, 0x4012F5A): with
    bpc = heads x spt / sectors_per_block, a badspot list of bpc + 1
    blocks at N - (bpc + 1) and a diagnostic area of 2 x bpc blocks before
    it; total_blocks = diag_daddr = N - (3 x bpc + 1), badspot_daddr = N -
    (bpc + 1), addr_end = N, 0xF6 = bpc, 0xF8 = 2 x bpc;
  - the name's last byte (0x27) NUL, 0xA4 = 2, 0xFA = 0 (0x401D9E4..).
- The kernel's mount (DISK_$PV_MOUNT: dinit with num_blocks 18, so DINIT
  returns no geometry) takes total_blocks, addr_end, blocks_per_track,
  tracks_per_cyl and sectors_per_block from the label: sector_size_code
  = log2(1) = 0, blocks_per_cyl = heads x spt = BAT_$MOUNT's allocation
  chunk (and a 16-bit divisor: invol --scsi refuses heads x spt above
  32767).  apollofs's BAT partitions are aligned to that cylinder.
- MAME's raw .hd loader has 512-byte sectors and guesses the CHS from the
  file size (src/lib/util/harddisk.cpp: the first sector count from the
  file's with sectors 63..2 and heads 16..2 dividing it), and its SCSI disk
  reports that guess in READ CAPACITY and pages 3 and 4.  An image for
  MAME needs a geometry the guess gives back (C x 16 x 63 always does);
  invol --scsi says when it does not.

Not reproduced: invol's placement of the LV (apollofs's layout is the same
as on its other volumes), the badspot list's contents (left zero).

## The SR10.2 DN300 volume (scripts/build_sr10.2_dn300.sh)

- Drive: PRIAM 7050 (dtype 0x105), 1049 cyl x 5 heads x 12 = 62940 blocks.
  MAME's DN300 ANSI disk model emulates a Priam 3450 (32 MB) and a Priam
  7050 ("64MB" type, chosen when the image has >= 60000 blocks); the 380 MB
  type is a DN3500 hack.  The 7050 is the largest real DN300 drive.
- Contents follow install/templates/apollo/os.v.10.2/aa.aegis_small: its 38
  subcomponents map (install/toc/toc.apollo.ST0141.c) to 18 tape files of
  the three std cartridges; every object of those tape files goes in at its
  path under install/ri.apollo.os.v.10.2, with its tape attributes (type
  UID, clocks, owner/group/org, rights).  Sources are the extracted tree;
  `apollofs ctindex --verify` showed it matches the tapes byte for byte.
- The ri tapes carry files only.  Directories, empty directories and the
  links the installer makes come from:
  - the SR10.2 *boot* cartridge (017286-001), which is the tree a
    tape-equipped node restores onto a fresh disk before installing: its
    directory records (attributes, default protections; this is where
    /sys/node_data/tmp, systmp, usrtmp, /sys/registry come from), its link
    records (/etc/rc -> `node_data/etc/rc, environ, ttys, sys.conf, ...,
    /sys/node_data/etc/environ -> environ.aegis, dm font links), and
    /sys/types, which no ri tape file has;
  - zero-size subcomponents of the template: /bscom (directory), /bin ->
    $(SYSTYPE)/bin, /dev -> `node_data/dev, /tmp -> `node_data/tmp;
  - links named in the release database (install/ri.apollo.os.v.10.2/
    ri.apollo.os.v.10.2) that an SR10.3 installed volume has identically
    (scripts/sr10.2_links.tsv lists every link with its provenance).
- sysboot goes in with cpboot (LV blocks 1..10, type sysboot).
- ov.aegis_small answers create_xapollo=no, so `node_data/etc/daemons/
  Xapollo (the marker /etc/rc starts X for instead of the DM) is left out,
  as on the boot cartridge.
- Emulator workaround: /sys/peb2_microcode is installed as
  peb2_microcode.do_not_use (as on the owner's SR10.3 dn300 disk); MAME
  answers the PEB probe but PEB_$LOAD_WCS then crashes with 00240009
  "wcs verify failed".

Regenerate the script (SOURCE_DATE_EPOCH, if set, is the clock every
apollofs command stamps with, which makes the image reproducible):

    apollofs ctindex ~/src/domainos-archeology/sr10.2/0172*.ct > manifest.tsv
    scripts/gen_build_sr10.2_dn300.py --manifest manifest.tsv \
        --install-root ~/src/domainos-archeology/sr10.2-install \
        --links scripts/sr10.2_links.tsv --out scripts/build_sr10.2_dn300.sh

## First boot: the registry (scripts/first_boot_sr10.2_dn300.sh)

The build is two stages: build_sr10.2_dn300.sh writes the volume, then
first_boot_sr10.2_dn300.sh boots it once under MAME (the original SR10.2
kernel, through the kernel repo's domain_os/tools/mame_boot_direct.py) and
types, at the Phase II `)` prompt, what an administrator of a new SR10.2
network types on its first node.  The SR10.2 tapes carry no registry; it is
made on the node:

- `sh` starts the single-process shell, which asks to log in.  With no
  registry the only login is `user.none.none` with an empty password
  (SR10.1 changes, 5.2.5 case 3: no server and no /sys/registry/rgy_local).
  Phase II's own `LO` loads a program without the global libraries, so it
  cannot run rgy_create.
- `/install/tools/rgy_create` (rgy_create(8); a boot-cartridge file the
  build installs) writes the master registry, /sys/registry/rgy_data/{acct,
  group, org, person, replicas, rgy, rgy_state, unix, update_log}.  Every
  reserved account (root, user, admin, daemon, ...) gets the password
  `-apollo-` (crypt w0UuY0sQV7sAI).
- `crf /etc/daemons/llbd`, `glbd`, `rgyd`: the stub files /etc/rc tests
  (`node_data/etc/daemons/README; rgyd and glbd require llbd).
- `/etc/server /etc/ncs/llbd`, then `/etc/ncs/glbd -create -first` creates
  the first GLB replica (`node_data/glb.e, glb.p; its log is
  `node_data/system_logs/glb_log).  glbd then serves in the foreground (run
  with `&` it dies at once), so the script quits it with ^C after ~40 s,
  leaves the shell with ^D and types Phase II's `shut`, which flushes and
  dismounts the volume ("Shutdown successful"); without it the image is
  left inconsistent.
- From then on `go` starts llbd, glbd and rgyd from /etc/rc ("Starting
  standard daemons: llbd glbd rgyd.").  At the DM `login:` prompt root /
  -apollo- is accepted (the DM then reports `user_data/startup_dm.19l` not
  found and opens the first pad) and a wrong password is refused.  Under
  MAME the kernel then crashes in the display interrupt (0013001D "invalid
  interrupt routine state", SMD_$DISP1_INT's BLT state 6, PC E26FD6): an
  emulation problem of the DN300 display, not of the volume.
- `apollofs check` on the result reports only /sys/node_data/systmp/
  global_rws blocks with zero headers (allocated, never written).

Typing uses plain key presses through the harness (Shift and Ctrl
included).  This needs the owner's patched DN300 keyboard in MAME (kernel-repo
bead source-jgxw: 8E1 framing and a code table whose codes are what SR10.2's
SMD_$KTT translates to each legend).  With an unpatched emulator, run with
TYPE_INJECT=1 to use the harness's --type-inject fallback.  At the default
typing rate a busy DM pad drops keys; DOS_KEY_HOLD=4 DOS_KEY_GAP=16 is reliable.

Not reproduced: extended ACL records (three objects on the boot tape),
node-specific answers of install++ (ov.* templates).

## The SR10.2 DN2500 volume (public-disk-images sau9)

The same contents as the DN300 volume (the aegis_small template holds every
SAU's /sauN; /sau9 has domain_os and the standalone utilities), built by
public-disk-images/sau9/scripts/build_sr10.2_dn2500.sh, generated by
gen_build_sr10.2_dn2500.py from the same manifest and links table:

- Disk: Apollo's 200 MB internal SCSI disk, the Series 2500's typical one
  (HP/Apollo Quick-Reference Configuration Guide, Jul 1990: 100 and 200 MB
  internal disks, the sample configuration has the 200 MB one), as
  400 x 16 x 63 = 403200 sectors, 206,438,400 bytes, a geometry MAME guesses
  back.  dtype 0x713, N = 201599, bpc = 1008, total_blocks 198574.
- `invol --scsi 400/16/63`, image dn2500_sr10.2.hd (raw), no
  peb2_microcode rename (no PEB on a DN2500).
