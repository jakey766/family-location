package storage

// Keep historical family IDs for installed clients; visibility and ownership are
// independent and deleting a family must not destroy device identities.
const familyAccessSchema = `
ALTER TABLE families ADD COLUMN deleted_at INTEGER;
ALTER TABLE families ADD COLUMN passcode_hash TEXT;
ALTER TABLE families ADD COLUMN passcode_version INTEGER NOT NULL DEFAULT 0;
CREATE TABLE device_admins (
 device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 PRIMARY KEY(device_id,user_id)
);
CREATE INDEX device_admins_user ON device_admins(user_id,device_id);
INSERT INTO device_admins SELECT d.id,fa.user_id FROM devices d JOIN family_admins fa ON fa.family_id=d.family_id;
CREATE TABLE family_browsers (
 token_hash BLOB PRIMARY KEY,
 expires_at INTEGER NOT NULL
);
CREATE INDEX family_browsers_expiry ON family_browsers(expires_at);
CREATE TABLE family_grants (
 token_hash BLOB NOT NULL REFERENCES family_browsers(token_hash) ON DELETE CASCADE,
 family_id INTEGER NOT NULL REFERENCES families(id) ON DELETE CASCADE,
 passcode_version INTEGER NOT NULL,
 expires_at INTEGER NOT NULL,
 PRIMARY KEY(token_hash,family_id)
);
CREATE INDEX family_grants_expiry ON family_grants(expires_at);
`
