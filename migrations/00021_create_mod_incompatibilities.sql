-- +goose Up
-- +goose StatementBegin
CREATE TABLE mod_incompatibilities (
  id INTEGER PRIMARY KEY,

  -- both sides reference mod_pages; order is arbitrary so we enforce
  -- mod_page_id_a < mod_page_id_b to prevent (A,B) and (B,A) duplicates
  mod_page_id_a INTEGER NOT NULL REFERENCES mod_pages(id) ON UPDATE CASCADE ON DELETE CASCADE,
  mod_page_id_b INTEGER NOT NULL REFERENCES mod_pages(id) ON UPDATE CASCADE ON DELETE CASCADE,

  -- user-provided reason (freeform; this is the whole point)
  reason TEXT,

  -- who flagged it
  source TEXT NOT NULL DEFAULT 'user' CHECK (source IN ('user')),

  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),

  CHECK (mod_page_id_a < mod_page_id_b),
  UNIQUE (mod_page_id_a, mod_page_id_b)
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_mod_incompatibilities_a ON mod_incompatibilities(mod_page_id_a);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_mod_incompatibilities_b ON mod_incompatibilities(mod_page_id_b);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_mod_incompatibilities_a;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_mod_incompatibilities_b;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE mod_incompatibilities;
-- +goose StatementEnd
