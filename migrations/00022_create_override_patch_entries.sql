-- +goose Up
-- +goose StatementBegin
CREATE TABLE override_patch_entries (
  id INTEGER PRIMARY KEY,
  override_id   INTEGER NOT NULL REFERENCES overrides(id) ON UPDATE CASCADE ON DELETE CASCADE,
  position      INTEGER NOT NULL CHECK (position >= 0),
  patch_type    TEXT NOT NULL CHECK (patch_type IN ('ini_set', 'ini_unset', 'json_set', 'json_unset', 'yaml_set', 'yaml_unset')),
  entry_section TEXT,
  entry_key     TEXT NOT NULL CHECK (LENGTH(entry_key) > 0),
  entry_value   TEXT, -- NULL for unset operations

  -- enforce that unset operations have no value and set operations do
  CHECK (
    (patch_type IN ('ini_set', 'json_set', 'yaml_set') AND entry_value IS NOT NULL)
    OR
    (patch_type IN ('ini_unset', 'json_unset', 'yaml_unset') AND entry_value IS NULL)
  ),

  -- section is only meaningful for ini types
  CHECK (
    patch_type LIKE 'ini%' OR entry_section IS NULL
  ),

  UNIQUE (override_id, position)
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_override_patch_entries_override ON override_patch_entries(override_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_override_patch_entries_override_pos ON override_patch_entries(override_id, position);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_override_patch_entries_override;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_override_patch_entries_override_pos;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE override_patch_entries;
-- +goose StatementEnd
