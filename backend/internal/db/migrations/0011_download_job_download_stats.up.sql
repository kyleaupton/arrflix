ALTER TABLE download_job
  ADD COLUMN download_speed BIGINT,
  ADD COLUMN eta_seconds    BIGINT,
  ADD COLUMN total_size     BIGINT;
