PRAGMA user_version = 2;

CREATE TABLE devices (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  token_hash   BLOB NOT NULL UNIQUE,
  status       TEXT NOT NULL CHECK (status IN ('pending', 'approved', 'revoked')),
  secret       BLOB,
  created_at   INTEGER NOT NULL,
  approved_at  INTEGER,
  last_seen_at INTEGER
);

CREATE TABLE transfers (
  id         TEXT PRIMARY KEY,
  device_id  TEXT NOT NULL REFERENCES devices(id),
  direction  TEXT NOT NULL CHECK (direction IN ('in', 'out')),
  name       TEXT NOT NULL,
  size       INTEGER NOT NULL,
  done       INTEGER NOT NULL DEFAULT 0,
  status     TEXT NOT NULL CHECK (status IN ('active', 'done', 'failed', 'canceled')),
  error      TEXT,
  path       TEXT,
  file_id    TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX transfers_by_device ON transfers(device_id, id DESC);
CREATE INDEX transfers_status ON transfers(status);

CREATE TABLE files (
  id         TEXT PRIMARY KEY,
  path       TEXT NOT NULL,
  name       TEXT NOT NULL,
  size       INTEGER NOT NULL,
  mod_time   INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);
