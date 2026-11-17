# apollofs notes: SR10 on-disk format, the SR10.2 DN300, DN2500 and DN3500 volumes

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

## ESDI volumes (invol -t 5nn)

Sources: the labels and reserved cylinders of two volumes an SR10.3 invol
made on MAME's DN3500 (OMTI 8621, Maxtor XT-4380E, dtype 0x504): an
installed SR10.3 volume and a freshly involed one, which agree but for
UIDs, times and names; MAME's devices/bus/isa/omti8621.cpp.

- The disk: the OMTI 8621 keeps Apollo block headers (1056-byte blocks)
  and reports one cylinder fewer than the drive table lists: MAME's
  Maxtor is 1223 x 15 x 18 (the table's 0x504 has 1224 cylinders) and its
  Micropolis 1023 x 8 x 18 (0x507: 1024), and the labels' addr_end is
  1223 x 270 = 330210.  The image is those cylinders, 348,701,760 bytes
  for the Maxtor, the size MAME creates; MAME takes an image of 300,000
  blocks or more as the Maxtor and anything smaller as the Micropolis.
- The PV label, as invol stores it: total_blocks = every cylinder but the
  last three (329400); the diagnostic area (0x90) in the two cylinders
  right after the volume and the badspot list (0x8C) in the last one;
  addr_end (0xA0) the whole disk; sector_size 1083; precomp 0x7FF; 0x9A
  0,0,1 and 0xC0 1, as on the DN300's; the name's last byte NUL.  0xF6 and
  0xF8 hold MAME's 0x55 creation fill on both volumes, so invol does not
  write them for this class; apollofs leaves them zero.
- The diagnostic cylinders are formatted: every byte of every block, the
  headers too, is 0x6C (the OMTI's FORMAT TRACK fill).
- The badspot cylinder: every block has a header of 01FF8302 0104BE9A
  01FFB16A, the time, 01FF0000, its own daddr (the first two words differ
  between the two volumes; not understood: DISK_$READ_MFG_BADSPOTS takes a
  block header error as ok) and zero data, except that the first block of
  tracks 0-4 holds the badspot list, empty (byte 0 = 1, the last four
  bytes FF FF FF 01), and the last block of tracks 10-14, the disk's last
  block among them, a copy of the PV label with its own daddr in the
  header.  cpboot rewrites the copies with the label.

Not reproduced on ESDI volumes: the words invol stores at 0x3F8 of the PV
label (0xFEDCA986) and the LV label (0xFEDCA987); the SR10.2 kernel boots
volumes without them.  The sau2 invol's volumes have them (below).

## The SR10.2 sau2 invol (invol -t for the other Winchesters)

Source: the SR10.2 standalone invol for sau2 (install/ri.apollo.os.v.10.2/
sau2/invol, loaded at 0x102000: file offset = address - 0x102000), traced
in Ghidra; the routine names are the ones given there (set_disk_layout,
process_disk, create_lv, ...).  apollofs makes every Winchester that is not
ESDI or SCSI (classes 0x000 to 0x400, the DN300's 0x103 to 0x105 among
them) as this invol does.

- The disk is the driver's block count (DISK_INIT: cylinders x heads x
  sectors per track; or TotalBlocks), the PV label's addr_end.
  set_disk_layout keeps the last cylinders out of the volume (bpc = blocks
  per cylinder):

  | class                     | volume ends | badspot list | diagnostic area |
  |---------------------------|-------------|--------------|-----------------|
  | 0x000, 0x100, 0x200       | 5 bpc back  | bpc back     | 2 bpc back      |
  | 0x300 to 0x600, any other | 3 bpc back  | bpc back     | 3 bpc back      |
  | 0x700                     | 3 bpc + 1   | bpc + 1 back | 3 bpc + 1 back  |

  (The class 0x500 row is the ESDI layout above; the class 0x700 row the
  SCSI one.)  So a whole PRIAM 7050 has total_blocks 62640, badspot_daddr
  62880, diag_daddr 62820, addr_end 62940.
- The interleave: set_disk_layout's default, by class: 0x000 to 0x300 2,
  0x400 to 0x700 1, any other class 2 (floppy 2, storage module type 0 9,
  others 5).  process_disk takes it as the LV interleave and create_lv
  stores it as a longword at LV label +0x3E: step_blocks 0, bat_step 2 on
  the DN300's disks.  BAT_$ALLOCATE (domain_os/bat/allocate.pas) spreads
  the blocks of one call that far apart, so the kernel can start the next
  read before the next sector passes.  apollofs lays files out the same
  way: on a volume whose step is above 1 it walks the bitmap as
  BAT_$ALLOCATE does for one call of all the object's blocks (stride,
  chunk and partition sweeps); with step 1 it keeps its contiguous runs.
  Step 0 is BAT_$MOUNT's 3.
- PV label (process_disk): the name blank padded, its last byte NUL;
  sector_start and sector_size the ANSI driver's words (0 and 1120 for
  0x104 and 0x105, 0 and 1181 for 0x103; the other classes keep apollofs's
  0/1056); precomp 0 and the word at 0xC0 0 (left as invol set them before
  the assign; not for the classes without the ANSI words); 0xA4 2;
  sectors_per_block 1 at 0xBE and 0x9E; 0xBC 0 (set only for a multi-disk
  group: not the sector interleave); 0xFEDCA986 at 0x3F8, 0 at 0x3FC.
- LV label (create_lv, SR10): the step above; free_blocks counted as
  create_lv frees the blocks (bat_free_block; it only starts at 0); only
  the mount time (0xB0) and last_valid_time (0xE6) of the times, with
  tz_name `UTC ` (0xE2): the node and the boot, dismount and current times
  are BAT_$MOUNT's; node_data's UID at 0xF4; 0xFEDCA987 at 0x3F8 and
  0xFFFFFFFF at 0x3FC (no alternate-label word).
- BAT partitions (create_lv): cyls = LV size / bpc; zones = ceil(cyls /
  (0x2800 / bpc)), 1 to 64; partition_start_offset = bpc - LV start mod
  bpc (0 when that is bpc); partition_size = ceil(cyls / zones) cylinders;
  zones left empty by the rounding are dropped; zones 0..n-1 state 1, then
  (n+1)/2 zones from (n+2)/4 state 2 (VTOCEs may go there).  The PRIAM
  7050: 7 zones of 149 cylinders.
- Root objects: / holds lost+found.list (an empty file), sys and sysboot
  (cpboot makes it here); // the node's entry; sys holds node_data, and
  node_data system_logs.  apollofs gives the new ones its default
  attributes; a later `mkdir -a` of one of these directories (the build
  script's tape records) gives it those attributes in place, as rbak does.
- Not reproduced: the placements create_lv computes (the BAT and the VTOC
  extents, the VTOC hash size: the SR10.3 layout above stands), the order
  UIDs are made in, invol's creation attributes of the root objects, and
  the alternate LV label word.

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
