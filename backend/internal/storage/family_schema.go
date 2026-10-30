package storage

// The original devices.family_id remains the immutable management family. The
// family_devices join table determines visibility independently of ownership.
const familySchema = `
ALTER TABLE families ADD COLUMN public_id TEXT;
UPDATE families SET public_id=lower(hex(randomblob(12)));
CREATE UNIQUE INDEX families_public_id ON families(public_id);
CREATE TABLE family_admins (
 family_id INTEGER NOT NULL REFERENCES families(id) ON DELETE CASCADE,
 user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 PRIMARY KEY(family_id,user_id)
);
CREATE INDEX family_admins_user ON family_admins(user_id,family_id);
INSERT INTO family_admins SELECT family_id,id FROM users;
CREATE TABLE family_devices (
 family_id INTEGER NOT NULL REFERENCES families(id) ON DELETE CASCADE,
 device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
 PRIMARY KEY(family_id,device_id)
);
CREATE INDEX family_devices_device ON family_devices(device_id,family_id);
INSERT INTO family_devices SELECT family_id,id FROM devices;
CREATE TABLE device_configs (
 device_id TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
 version INTEGER NOT NULL DEFAULT 1 CHECK(version BETWEEN 1 AND 9007199254740991),
 moving_interval_seconds INTEGER NOT NULL DEFAULT 120 CHECK(moving_interval_seconds BETWEEN 60 AND 3600 AND moving_interval_seconds%60=0),
 stationary_interval_seconds INTEGER NOT NULL DEFAULT 300 CHECK(stationary_interval_seconds BETWEEN moving_interval_seconds AND 3600 AND stationary_interval_seconds%60=0),
 moving_high_accuracy INTEGER NOT NULL DEFAULT 0 CHECK(moving_high_accuracy IN (0,1)),
 quiet_hours_start TEXT NOT NULL DEFAULT '22:00' CHECK(length(quiet_hours_start)=5 AND quiet_hours_start GLOB '[0-2][0-9]:[0-5][0-9]' AND quiet_hours_start<'24:00'),
 quiet_hours_end TEXT NOT NULL DEFAULT '08:00' CHECK(length(quiet_hours_end)=5 AND quiet_hours_end GLOB '[0-2][0-9]:[0-5][0-9]' AND quiet_hours_end<'24:00' AND quiet_hours_end<>quiet_hours_start)
);
INSERT INTO device_configs(device_id) SELECT id FROM devices;
`
