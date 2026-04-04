-- +goose Up
-- +goose StatementBegin
ALTER TABLE game_installs ADD COLUMN notes TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE game_installs DROP COLUMN notes;
-- +goose StatementEnd
