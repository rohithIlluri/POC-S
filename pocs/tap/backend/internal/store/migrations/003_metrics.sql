-- Pilot metrics: one row per claim outcome, and each device's platform so
-- cross-platform taps can be counted.

ALTER TABLE devices ADD COLUMN platform text NOT NULL DEFAULT '';

CREATE TABLE tap_attempts (
    id          text PRIMARY KEY,
    at          timestamptz NOT NULL,
    user_id     text NOT NULL REFERENCES users ON DELETE CASCADE,
    device_id   text NOT NULL,
    transport   text NOT NULL,
    distance_cm integer NOT NULL,
    outcome     text NOT NULL, -- matched | retry | expired | rejected
    code        text NOT NULL DEFAULT '',
    wait_ms     bigint NOT NULL
);
CREATE INDEX tap_attempts_at ON tap_attempts (at);
CREATE INDEX taps_created ON taps (created_at);
