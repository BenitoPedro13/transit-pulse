-- +goose Up
-- feed-status.v1, one row per (feed, minute). A plain table kept forever: it is the coverage
-- history that tells "no data" from "no vehicles" (invariant 6).
CREATE TABLE feed_status (
  feed                   text        NOT NULL,
  minute_utc             timestamptz NOT NULL,
  operator               text        NOT NULL,
  status                 text        NOT NULL CHECK (status IN ('ok', 'failed', 'provider_not_ok', 'gap')),
  records                integer     NOT NULL,
  events                 integer     NOT NULL,
  invalid                integer     NOT NULL,
  unmapped               integer     NOT NULL,
  conflicting_duplicates integer     NOT NULL,
  provider_status        text,
  raw_keys               text[]      NOT NULL,
  PRIMARY KEY (feed, minute_utc)
);

-- +goose Down
DROP TABLE feed_status;
