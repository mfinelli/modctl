-- +goose Up
-- +goose StatementBegin
ALTER TABLE game_installs ADD COLUMN applied_profile_id INTEGER
  REFERENCES profiles(id) ON UPDATE CASCADE ON DELETE SET NULL;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE game_installs ADD COLUMN applied_at TEXT;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE game_installs ADD COLUMN applied_operation_id INTEGER
  REFERENCES operations(id) ON UPDATE CASCADE ON DELETE SET NULL;
-- +goose StatementEnd

-- +goose StatementBegin
-- applied_profile_id must be NULL or refer to a profile for the same game_install_id
CREATE TRIGGER trg_game_installs_applied_profile_matches_game_ins
BEFORE INSERT ON game_installs
FOR EACH ROW
  WHEN NEW.applied_profile_id IS NOT NULL
BEGIN
  SELECT
  CASE
    WHEN NOT EXISTS (
      SELECT 1
      FROM profiles p
      WHERE p.id = NEW.applied_profile_id
        AND p.game_install_id = NEW.id
    )
    THEN RAISE(ABORT, 'applied_profile_id must belong to the same game_install')
  END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_game_installs_applied_profile_matches_game_upd
BEFORE UPDATE OF applied_profile_id ON game_installs
FOR EACH ROW
  WHEN NEW.applied_profile_id IS NOT NULL
BEGIN
  SELECT
  CASE
    WHEN NOT EXISTS (
      SELECT 1
      FROM profiles p
      WHERE p.id = NEW.applied_profile_id
        AND p.game_install_id = NEW.id
    )
    THEN RAISE(ABORT, 'applied_profile_id must belong to the same game_install')
  END;
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER trg_game_installs_applied_profile_matches_game_upd;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER trg_game_installs_applied_profile_matches_game_ins;
-- +goose StatementEnd

-- TODO: rebuild the game_installs table without the columns we added
-- https://stackoverflow.com/a/66399224
