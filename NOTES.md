# apollofs notes: SR10 on-disk format and the SR10.2 DN300 volume

## On-disk format (as written and checked by apollofs)

Sources: the kernel reconstruction in domainos-archeology/source/domain_os
(bat/, vtoc/, dir/, ast/, disk/, os/) and an SR10.3 volume an installer wrote.

- Blocks are 1056 bytes: a 32-byte header (object UID, page within object,
  clockh written, system-type word, physical daddr) and 1024 data bytes.
  Data pages: system-type 0 (files) or sub type << 16 (directory pages);
  file-map blocks: level << 24, page = first page mapped (32 for a level-1
  root, 288 for a level-2 root).  System blocks (labels, BAT, VTOC) use
  page = their own daddr.
- PV label (block 0) gains SR10 fields at 0xA0..0xC4 (addr_end, striping
  set, interleave, sectors_per_block = 1, invol's revision string).
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

Regenerate the script:

    apollofs ctindex ~/src/domainos-archeology/sr10.2/0172*.ct > manifest.tsv
    scripts/gen_build_sr10.2_dn300.py --manifest manifest.tsv \
        --install-root ~/src/domainos-archeology/sr10.2-install \
        --links scripts/sr10.2_links.tsv --out scripts/build_sr10.2_dn300.sh

Not reproduced: extended ACL records (three objects on the boot tape),
the registry (rgy_create builds it on a running node), node-specific
answers of install++ (ov.* templates).
