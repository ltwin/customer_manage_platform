-- Importing into the library asks whether the same bytes are already there.
CREATE INDEX creative_blob_digest ON creative_blobs(account_id,sha256) WHERE state='ready';
