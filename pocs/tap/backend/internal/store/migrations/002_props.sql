-- Props: weekly compliment polls among people who have tapped.

CREATE TABLE props_rounds (
    user_id    text NOT NULL REFERENCES users ON DELETE CASCADE,
    week       text NOT NULL,
    created_at timestamptz NOT NULL,
    polls      jsonb NOT NULL,
    PRIMARY KEY (user_id, week)
);

-- voter_id is kept to honour removals and deletions; no API returns it.
CREATE TABLE props_votes (
    id         text PRIMARY KEY,
    voter_id   text NOT NULL REFERENCES users ON DELETE CASCADE,
    picked_id  text NOT NULL REFERENCES users ON DELETE CASCADE,
    prompt_id  text NOT NULL,
    week       text NOT NULL,
    event_id   text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL
);
CREATE INDEX props_votes_picked ON props_votes (picked_id, created_at);
