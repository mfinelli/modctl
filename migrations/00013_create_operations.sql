-- +goose Up
-- +goose StatementBegin
CREATE TABLE operations
-- journal of apply/unapply/switch runs
(
  id INTEGER PRIMARY KEY,
  game_install_id INTEGER NOT NULL REFERENCES game_installs(id) ON UPDATE CASCADE ON DELETE CASCADE,
  -- the profile that was intended/applied
  profile_id INTEGER REFERENCES profiles(id) ON UPDATE CASCADE ON DELETE SET NULL,

  -- what kind of run this was
  op_type TEXT NOT NULL CHECK (op_type IN ('apply', 'unapply')),

  -- lifecycle status
  status TEXT NOT NULL CHECK (status IN ('running', 'success', 'failed')),

  started_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  finished_at TEXT,

  -- optional freeform error message or summary
  message TEXT,

  -- optional structured data (counts, planner summary, etc)
  metadata TEXT CHECK (json_valid(metadata))
) STRICT;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_operations_game_install ON operations(game_install_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_operations_profile ON operations(profile_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_operations_game_started ON operations(game_install_id, started_at DESC);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_operations_profile_started ON operations(profile_id, started_at DESC);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_operations_status ON operations(status);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX idx_operations_status;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_operations_profile_started;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_operations_game_started;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_operations_profile;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX idx_operations_game_install;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE operations;
-- +goose StatementEnd
