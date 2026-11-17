-- Hardlink facts on file_state, captured from stat() of the placed file:
-- st_dev and st_ino identify the inode, st_nlink counts the names it has.
-- Nullable: a row written before these columns existed has none until a
-- scan re-stats the file. The kernel's unsigned values are stored bit-for-bit
-- in BIGINT, so a value above 2^63-1 reads back negative but compares equal.
ALTER TABLE file_state
  ADD COLUMN st_dev BIGINT,
  ADD COLUMN st_ino BIGINT,
  ADD COLUMN st_nlink BIGINT;
