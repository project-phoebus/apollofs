# apollofs

A tool for interacting with Apollo computer filesystem images.

Using both of these for reference, but they both apparently document only the SR9 filesystem.  I've been able to figure out from decompiling the kernel _some_ of the SR10 filesystem structures (enough to implement the `list` command and `copyout`):

* http://www.bitsavers.org/pdf/apollo/AEGIS_Internals_and_Data_Structures_Jan86.pdf
* http://www.bitsavers.org/pdf/apollo/002398-04_Domain_Engineering_Handbook_Rev4_Jan87.pdf

Usage:
```
% ./apollofs
A tool for interacting with Apollo filesystems

Usage:
  apollofs [command]

Available Commands:
  check       Check the volume's VTOC, file maps, directories and BAT for consistency (read-only)
  completion  Generate the autocompletion script for the specified shell
  copyin      Copy a file from the host to the disk image
  copyout     Copy a file from the disk image to the host
  cpboot      Make the disk image bootable (copy sysboot to /sysboot, LV blocks 1-10)
  crl         Create a soft link (like /com/crl): path names the link, text is what it resolves to
  ctindex     List the objects on rbak cartridge-tape images with their tape file, type and VTOCE attributes
  help        Help about any command
  info        Dump information about filesystem structures
  invol       Initialize a disk image (PV/LV labels, BAT, VTOC, / and //)
  list        List files/directories in the disk image (similar to 'ls' in the host)
  mkdir       Create a directory in the disk image (similar to 'mkdir' in the host)
  rm          Delete a file, a link or an empty directory

Flags:
  -d, --debug              Enable debug output
  -i, --diskImage string   Path to disk image (required)
  -h, --help               help for apollofs

Use "apollofs [command] --help" for more information about a command.
```

Building a volume from scratch (see NOTES.md and scripts/build_sr10.2_dn300.sh):

```
% apollofs -i new.awd invol -t 105 --name DN300          # PRIAM 7050, with a 1 MB OS paging file
% apollofs -i new.awd cpboot sysboot
% apollofs -i new.awd mkdir /lib
% apollofs -i new.awd copyin -t coff libc /lib/libc       # or -a <attribute image from ctindex>
% apollofs -i new.awd copyin --replace -t coff libc /lib/libc
% apollofs -i new.awd crl /tmp '`node_data/tmp'
% apollofs -i new.awd check
```

### dumping labels (physical and logical)
```
% ./apollofs -i dn3500_sr10.4.awd info labels
Disk image: ../dn3500_sr10.4.awd
PV Label:
Version: 1
Name: APOLLO
UID: 10012345.776175af
DriveType: 1284
TotalBlocksInVolume: 329400
BlocksPerTrack: 18
TracksPerCylinder: 15
SectorStart: 5
SectorSize: 2260
PreComp: 5
Logical Volumes:
  LV0: block 1 / 329399 (alt)
...
LV Label:
Version: 1
Name:
ID: 20012345.776175d5
Label Written: Sun, 20 Nov 2044 18:44:46 UTC
Boot Time: Sun, 20 Nov 2044 16:03:05 UTC
Dismounted Time: Sun, 20 Nov 2044 16:03:23 UTC
VTOCHeader:
  Version: 2
  VTOCSizeInBlocks: 3623
  VTOCBlocksUsed: 907
  NetworkRootDirVTOCX: vtoc blk 160110, index 2
  DiskEntryDirVTOCX: vtoc blk 160110, index 1
  OSPagingFileVTOCX: vtoc blk 160111, index 1
  SysbootVTOCX: vtoc blk 160109, index 0
...
%
```

### dumping blocks (referenced by their physical daddr, not relative to a logical volume)
```
% ./apollofs -i ../dn3500_sr10.4.awd info block 160111
Disk image: ../dn3500_sr10.4.awd
BlockHeader:
  object uid: 0000.0202 (vtoc_$uid)
  Page within object: 160110
  Block type: 0
  System type: 0
  Checksum: 0
  Block DAddr: 160111
%
```


### listing directories and files
```
% ./apollofs -i ../dn3500_sr10.4.awd list /
.rhosts
bin -> $(SYSTYPE)/bin
bscom
bsd4.2 -> bsd4.3
bsd4.3
com
dev -> `node_data/dev
dn300-disk
domain_examples
etc
install
lib
lost+found
lost+found.list
sau2
sau2-copied-from-sau7
sau7
sau_sys
sys
sys5 -> sys5.3
sys5.3
sysboot
sysboot.m68k
systest
tmp -> `node_data/tmp
user_data
usr
```

### copying files from the disk image to the host filesystem
```
% ./apollofs -i ../dn3500_sr10.4.awd copyout /sau7/domain_os ./domain_os.sau7
Copying 927 blocks (948332 bytes) from /sau7/domain_os to ./domain_os.sau7...
done.
% ls -l ./domain_os.sau7
-rw-r--r--  1 toshok  staff  948332 Jul 11 14:13 ./domain_os.sau7
```