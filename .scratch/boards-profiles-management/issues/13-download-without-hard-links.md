# 13: Download to file systems without hard links

**What to build:** Decide whether `images download` should support target file systems that lack hard links, such as FAT32 and exFAT, which are common on USB drives and SD cards.

**Blocked by:** None.

**Status:** deferred

## Current behavior

Ticket 06 publishes the download with `os.Link(temp, target)`, a create-exclusive operation. On a file system without hard links, the link fails and the result is `output_write_failed` with the absolute `path`. The temporary file is removed. Nothing is overwritten, and no partial file is left at the target. Linux, macOS, and Windows NTFS are unaffected.

## Option recorded for later

When linking fails because the file system does not support it, Bediz could fall back to creating the target with `O_CREATE|O_EXCL` and copying the temporary file into it. The trade-off:

- It is still create-exclusive, so an existing target is never replaced.
- It is not atomic. A partial file is visible at the target while the copy runs. If the copy fails, Bediz must remove the target it created, which a crash can prevent.

Adopting the fallback changes the no-partial-output guarantee in spec §15.1, so the user decides. On 2026-09-24 the user chose to keep the current behavior for now.
