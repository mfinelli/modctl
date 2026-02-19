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

-- +goose StatementBegin
-- backups: ensure target_id belongs to game_install_id
CREATE TRIGGER trg_backups_target_matches_install_ins
BEFORE INSERT ON backups
FOR EACH ROW
BEGIN
  SELECT
  CASE
    WHEN (SELECT 1 FROM targets t
          WHERE t.id = NEW.target_id
            AND t.game_install_id = NEW.game_install_id) IS NULL
    THEN RAISE(ABORT, 'backups: target_id does not belong to game_install_id')
  END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_backups_target_matches_install_upd
BEFORE UPDATE OF target_id, game_install_id ON backups
FOR EACH ROW
BEGIN
  SELECT
  CASE
    WHEN (SELECT 1 FROM targets t
          WHERE t.id = NEW.target_id
            AND t.game_install_id = NEW.game_install_id) IS NULL
    THEN RAISE(ABORT, 'backups: target_id does not belong to game_install_id')
  END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
-- operation_changes: ensure target_id belongs to game_install_id
CREATE TRIGGER trg_opchg_target_matches_install_ins
BEFORE INSERT ON operation_changes
FOR EACH ROW
BEGIN
  SELECT
  CASE
    WHEN (SELECT 1 FROM targets t
          WHERE t.id = NEW.target_id
            AND t.game_install_id = NEW.game_install_id) IS NULL
    THEN RAISE(ABORT, 'operation_changes: target_id does not belong to game_install_id')
  END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
-- operation_changes: ensure target_id belongs to game_install_id
CREATE TRIGGER trg_opchg_target_matches_install_upd
BEFORE UPDATE OF target_id, game_install_id ON operation_changes
FOR EACH ROW
BEGIN
  SELECT
  CASE
    WHEN (SELECT 1 FROM targets t
          WHERE t.id = NEW.target_id
            AND t.game_install_id = NEW.game_install_id) IS NULL
    THEN RAISE(ABORT, 'operation_changes: target_id does not belong to game_install_id')
  END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
-- overrides: ensure profile and target belong to the same game_install
--
-- This checks two things:
--  1) target_id belongs to the same game_install as the profile
--  2) (implicitly) both exist (FKs already ensure existence)
CREATE TRIGGER trg_overrides_profile_target_same_install_ins
BEFORE INSERT ON overrides
FOR EACH ROW
BEGIN
  SELECT
  CASE
    WHEN (SELECT 1
          FROM profiles p
          JOIN targets t ON t.id = NEW.target_id
          WHERE p.id = NEW.profile_id
            AND t.game_install_id = p.game_install_id) IS NULL
    THEN RAISE(ABORT, 'overrides: target_id does not belong to the same game_install as profile_id')
  END;
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER trg_overrides_profile_target_same_install_upd
BEFORE UPDATE OF profile_id, target_id ON overrides
FOR EACH ROW
BEGIN
  SELECT
  CASE
    WHEN (SELECT 1
          FROM profiles p
          JOIN targets t ON t.id = NEW.target_id
          WHERE p.id = NEW.profile_id
            AND t.game_install_id = p.game_install_id) IS NULL
    THEN RAISE(ABORT, 'overrides: target_id does not belong to the same game_install as profile_id')
  END;
END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER trg_overrides_profile_target_same_install_upd;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER trg_overrides_profile_target_same_install_ins;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER trg_opchg_target_matches_install_upd;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER trg_opchg_target_matches_install_ins;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER trg_backups_target_matches_install_upd;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER trg_backups_target_matches_install_ins;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER trg_installed_files_target_matches_install_upd;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER trg_installed_files_target_matches_install_ins;
-- +goose StatementEnd
