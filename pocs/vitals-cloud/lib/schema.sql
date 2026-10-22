-- Mirrors the Go strapdb buckets. Every write is an idempotent upsert.
CREATE TABLE IF NOT EXISTS hr (
  ts_ms     bigint PRIMARY KEY,           -- unix ms
  bpm       smallint NOT NULL,
  rr        integer[] NOT NULL DEFAULT '{}', -- beat-to-beat ms
  source    text NOT NULL DEFAULT '',
  off_wrist boolean NOT NULL DEFAULT false
);
CREATE TABLE IF NOT EXISTS events (
  ts_ms bigint NOT NULL,
  num   integer NOT NULL,
  data  text NOT NULL DEFAULT '',          -- hex
  PRIMARY KEY (ts_ms, num)
);
CREATE TABLE IF NOT EXISTS records (
  num  bigint PRIMARY KEY,                 -- history record number
  body text NOT NULL                       -- raw 0x2F body, hex
);
CREATE TABLE IF NOT EXISTS device (
  id         text PRIMARY KEY,             -- relay-chosen id of the paired strap
  name       text NOT NULL DEFAULT '',
  address    text NOT NULL DEFAULT '',     -- BLE address remembered after the one-time bond
  bonded_at  timestamptz,
  last_seen  timestamptz
);
