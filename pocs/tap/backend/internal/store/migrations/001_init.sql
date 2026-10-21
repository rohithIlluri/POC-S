-- Tap pilot schema. Mirrors the design's data model plus the tables the
-- build plan adds (taps, nonces, pending claims, sessions, blocks, reports,
-- event membership).

CREATE TABLE users (
    id           text PRIMARY KEY,
    display_name text NOT NULL,
    photo_url    text NOT NULL DEFAULT '',
    phone_hash   text NOT NULL UNIQUE,
    created_at   timestamptz NOT NULL,
    frozen       boolean NOT NULL DEFAULT false
);

CREATE TABLE sessions (
    token   text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users ON DELETE CASCADE,
    expires timestamptz NOT NULL
);

CREATE TABLE devices (
    id            text PRIMARY KEY,
    user_id       text NOT NULL REFERENCES users ON DELETE CASCADE,
    public_key    text NOT NULL,
    attest_status text NOT NULL,
    created_at    timestamptz NOT NULL
);

CREATE TABLE nonces (
    value     text PRIMARY KEY,
    device_id text NOT NULL,
    issued_at timestamptz NOT NULL,
    used      boolean NOT NULL DEFAULT false
);

CREATE TABLE pending_claims (
    pair_key    text NOT NULL,
    device_id   text NOT NULL,
    user_id     text NOT NULL,
    claim       jsonb NOT NULL,
    issued_at   timestamptz NOT NULL,
    received_at timestamptz NOT NULL,
    PRIMARY KEY (pair_key, device_id)
);

-- One row per accepted handshake; user ids become 'deleted' when an
-- account is erased so the other side's tap history keeps its shape.
CREATE TABLE taps (
    seq        bigserial UNIQUE,
    id         text PRIMARY KEY,
    user_a     text NOT NULL,
    user_b     text NOT NULL,
    device_a   text NOT NULL,
    device_b   text NOT NULL,
    created_at timestamptz NOT NULL,
    geohash    text NOT NULL,
    geohash6   text NOT NULL,
    event_id   text NOT NULL DEFAULT '',
    pair_key   text NOT NULL UNIQUE,
    receipt    jsonb NOT NULL,
    signature  text NOT NULL
);
CREATE INDEX taps_near ON taps (geohash6, created_at);
CREATE INDEX taps_device_a ON taps (device_a, created_at);
CREATE INDEX taps_device_b ON taps (device_b, created_at);

-- user_a < user_b in byte order, as Go's store.Pair compares them. COLLATE
-- "C" keeps the check and key independent of the server's locale (under
-- en_US, 'usr_U…' sorts after 'usr_t…' and the check would reject a pair).
CREATE TABLE edges (
    user_a       text COLLATE "C" NOT NULL REFERENCES users ON DELETE CASCADE,
    user_b       text COLLATE "C" NOT NULL REFERENCES users ON DELETE CASCADE,
    created_at   timestamptz NOT NULL,
    first_tap_id text NOT NULL,
    last_tap_at  timestamptz NOT NULL,
    tap_count    integer NOT NULL,
    status       text NOT NULL,
    PRIMARY KEY (user_a, user_b),
    CHECK (user_a < user_b)
);
CREATE INDEX edges_b ON edges (user_b);

CREATE TABLE edge_meta (
    owner_id   text NOT NULL REFERENCES users ON DELETE CASCADE,
    other_id   text NOT NULL REFERENCES users ON DELETE CASCADE,
    note       text NOT NULL DEFAULT '',
    hide_place boolean NOT NULL DEFAULT false,
    PRIMARY KEY (owner_id, other_id)
);

CREATE TABLE blocks (
    blocker_id text NOT NULL REFERENCES users ON DELETE CASCADE,
    blocked_id text NOT NULL REFERENCES users ON DELETE CASCADE,
    PRIMARY KEY (blocker_id, blocked_id)
);

CREATE TABLE reports (
    id          text PRIMARY KEY,
    reporter_id text NOT NULL REFERENCES users ON DELETE CASCADE,
    subject_id  text NOT NULL REFERENCES users ON DELETE CASCADE,
    reason      text NOT NULL,
    created_at  timestamptz NOT NULL
);
CREATE INDEX reports_subject ON reports (subject_id);

CREATE TABLE events (
    id        text PRIMARY KEY,
    name      text NOT NULL DEFAULT '',
    geohash6  text NOT NULL,
    start_at  timestamptz NOT NULL,
    end_at    timestamptz NOT NULL,
    tap_count integer NOT NULL
);
CREATE INDEX events_near ON events (geohash6, end_at);

CREATE TABLE event_members (
    event_id text NOT NULL REFERENCES events ON DELETE CASCADE,
    user_id  text NOT NULL REFERENCES users ON DELETE CASCADE,
    PRIMARY KEY (event_id, user_id)
);
CREATE INDEX event_members_user ON event_members (user_id);
