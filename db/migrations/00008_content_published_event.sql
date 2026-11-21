-- +goose Up
-- P3-01b: 記事・作品の公開を通知の種別に加える（タイトルと種別だけ。公開済みの情報なので機密なし）。
ALTER TABLE notification_settings DROP CONSTRAINT notification_settings_event_valid;
ALTER TABLE notification_settings ADD CONSTRAINT notification_settings_event_valid CHECK (event_type IN ('inventory.loan_created', 'inventory.loan_returned', 'inventory.correction_created', 'content.published'));
INSERT INTO notification_settings (event_type) VALUES ('content.published');

-- +goose Down
DELETE FROM notification_outbox WHERE event_type = 'content.published';
DELETE FROM notification_settings WHERE event_type = 'content.published';
ALTER TABLE notification_settings DROP CONSTRAINT notification_settings_event_valid;
ALTER TABLE notification_settings ADD CONSTRAINT notification_settings_event_valid CHECK (event_type IN ('inventory.loan_created', 'inventory.loan_returned', 'inventory.correction_created'));
