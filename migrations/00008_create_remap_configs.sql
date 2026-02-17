-- +goose Up
-- +goose StatementBegin
CREATE TABLE remap_configs
-- container for a sequence of remap rules
(
  id INTEGER PRIMARY KEY,

  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP_TABLE remap_configs;
-- +goose StatementEnd
