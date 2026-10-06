-- +goose Up
-- +goose StatementBegin
-- targets can be disabled so that apply/unapply planning skips them (useful
-- for games that only use one of game_dir/proton_prefix). Existing targets
-- stay enabled.
ALTER TABLE targets ADD COLUMN enabled INTEGER NOT NULL DEFAULT TRUE CHECK (enabled IN (TRUE, FALSE));
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE targets DROP COLUMN enabled;
-- +goose StatementEnd
