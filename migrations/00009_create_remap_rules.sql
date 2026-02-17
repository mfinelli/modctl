-- +goose Up
-- +goose StatementBegin
CREATE TABLE remap_rules
-- remap_rules: ordered rules belonging to a remap_config
--
-- Rule semantics (v1):
-- - strip_components: int_value = N (>=0)
-- - select_subdir:    text_value = subdir path (relative, no leading '/')
-- - dest_prefix:      text_value = destination prefix (relative)
-- - include_glob:     text_value = glob pattern
-- - exclude_glob:     text_value = glob pattern
--
-- The planner/extractor applies rules in ascending position order.
(
  id INTEGER PRIMARY KEY,
  remap_config_id INTEGER NOT NULL REFERENCES remap_configs(id) ON UPDATE CASCADE ON DELETE CASCADE,
  position INTEGER NOT NULL CHECK (position >= 0),

  rule_type TEXT NOT NULL CHECK (rule_type IN (
      'strip_components',
      'select_subdir',
      'dest_prefix',
      'include_glob',
      'exclude_glob'
    )),

  -- parameter payload (normalized-ish)
  int_value INTEGER,
  text_value TEXT,

  -- optional future extension hook without new table
  json_value TEXT CHECK (json_valid(json_value)),

  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

  -- enforce deterministic odering and no duplicates
  UNIQUE(remap_config_id, position),

  -- enforce that each rule has the right parameter shape
  CHECK (
    CASE rule_type
      WHEN 'strip_components' THEN int_value IS NOT NULL AND int_value >= 0 AND text_value IS NULL AND json_value IS NULL
      WHEN 'select_subdir'    THEN text_value IS NOT NULL AND LENGTH(text_value) > 0 AND int_value IS NULL AND json_value IS NULL
      WHEN 'dest_prefix'      THEN text_value IS NOT NULL AND LENGTH(text_value) > 0 AND int_value IS NULL AND json_value IS NULL
      WHEN 'include_glob'     THEN text_value IS NOT NULL AND LENGTH(text_value) > 0 AND int_value IS NULL AND json_value IS NULL
      WHEN 'exclude_glob'     THEN text_value IS NOT NULL AND LENGTH(text_value) > 0 AND int_value IS NULL AND json_value IS NULL
      ELSE 0
    END
  )
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_remap_rules_config ON remap_rules(remap_config_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_remap_rules_config_pos ON remap_rules(remap_config_id, position);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_remap_rules_type ON remap_rules(rule_type);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_remap_rules_type;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_remap_rules_config_pos;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_remap_rules_config;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE remap_rules;
-- +goose StatementEnd
