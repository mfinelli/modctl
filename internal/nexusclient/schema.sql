CREATE TABLE IF NOT EXISTS cache_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) STRICT, WITHOUT ROWID;

INSERT OR IGNORE INTO cache_meta (key, value) VALUES ('schema_version', '1');

CREATE TABLE IF NOT EXISTS nexus_mod_info (
  nexus_game_domain  TEXT NOT NULL,
  nexus_mod_id       INTEGER NOT NULL,
  fetched_at         TEXT NOT NULL,
  name               TEXT,
  summary            TEXT,
  author             TEXT,
  is_available       INTEGER CHECK (is_available IS NULL OR is_available IN (TRUE, FALSE)),
  raw_json           TEXT CHECK (raw_json IS NULL OR json_valid(raw_json)),

  PRIMARY KEY (nexus_game_domain, nexus_mod_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS nexus_file_info (
  nexus_game_domain  TEXT NOT NULL,
  nexus_mod_id       INTEGER NOT NULL,
  nexus_file_id      INTEGER NOT NULL,
  fetched_at         TEXT NOT NULL,
  name               TEXT,
  version            TEXT,
  category_name      TEXT,
  is_primary         INTEGER CHECK (is_primary IS NULL OR is_primary IN (TRUE, FALSE)),
  file_name          TEXT,
  size_in_bytes      INTEGER,
  uploaded_timestamp INTEGER,
  raw_json           TEXT CHECK (raw_json IS NULL OR json_valid(raw_json)),

  PRIMARY KEY (nexus_game_domain, nexus_mod_id, nexus_file_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE IF NOT EXISTS nexus_file_updates (
  nexus_game_domain  TEXT NOT NULL,
  nexus_mod_id       INTEGER NOT NULL,
  old_file_id        INTEGER NOT NULL,
  new_file_id        INTEGER NOT NULL,
  uploaded_timestamp INTEGER NOT NULL,
  fetched_at         TEXT NOT NULL,

  PRIMARY KEY (nexus_game_domain, nexus_mod_id, old_file_id)
) STRICT, WITHOUT ROWID;
