-- +goose Up
-- +goose StatementBegin
-- Enforce relational consistency between game_install_id and target_id (and profile_id)
-- This prevents bugs where a row references a target from a different game_install.
-- installed_files: ensure target_id belongs to game_install_id
CREATE TRIGGER trg_installed_files_target_matches_install_ins
BEFORE INSERT ON installed_files
FOR EACH ROW
BEGIN
  SELECT
  CASE
    WHEN (SELECT 1 FROM targets t
          WHERE t.id = NEW.target_id
            AND t.game_install_id = NEW.game_install_id) IS NULL
    THEN RAISE(ABORT, 'installed_files: target_id does not belong to game_install_id')
  END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_installed_files_target_matches_install_upd
BEFORE UPDATE OF target_id, game_install_id ON installed_files
FOR EACH ROW
BEGIN
  SELECT
  CASE
    WHEN (SELECT 1 FROM targets t
          WHERE t.id = NEW.target_id
            AND t.game_install_id = NEW.game_install_id) IS NULL
    THEN RAISE(ABORT, 'installed_files: target_id does not belong to game_install_id')
  END;
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER trg_installed_files_target_matches_install_upd;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER trg_installed_files_target_matches_install_ins;
-- +goose StatementEnd
