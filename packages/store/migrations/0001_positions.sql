-- +goose Up
-- positions.v1, one row per identity (operator, feed, vehicle, event_time). Nulls stay nulls
-- (missing is not zero). speed_kmh and heading_deg are double precision: the provider sends
-- fractional values (e.g. direcao 111.77) that a 4-byte real would not round-trip
-- (docs/research/README.md §1.1).
--
-- A hypertable on event_time with 1-day chunks. The columnstore is off on purpose: INSERT ... ON
-- CONFLICT on compressed chunks is unreliable, and repeats arrive up to 70 minutes late
-- (docs/tasks/TASK-positions-consumer.md §2.1 #2). No default time index: nothing queries by time
-- alone yet, and bytes per row is the risk. The primary key includes event_time, which a unique
-- index on a hypertable requires.
CREATE TABLE positions (
  operator      text             NOT NULL,
  feed          text             NOT NULL,
  vehicle       text             NOT NULL,
  event_time    timestamptz      NOT NULL,
  city          text             NOT NULL,
  mode          text             NOT NULL,
  authority     text             NOT NULL,
  source_kind   text             NOT NULL,
  line          text,
  route_id      text,
  lat           double precision NOT NULL,
  lon           double precision NOT NULL,
  speed_kmh     double precision,
  heading_deg   double precision,
  received_time timestamptz      NOT NULL,
  raw_key       text             NOT NULL,
  raw_minute    timestamptz      NOT NULL,
  raw_page      integer          NOT NULL,
  PRIMARY KEY (operator, feed, vehicle, event_time)
) WITH (
  tsdb.hypertable = true,
  tsdb.partition_column = 'event_time',
  tsdb.chunk_interval = '1 day',
  tsdb.columnstore = false,
  tsdb.create_default_indexes = false
);

-- Full detail is kept for 3 days (architecture §8). Retention drops whole chunks, so up to 4 days
-- are on disk at any time.
SELECT add_retention_policy('positions', drop_after => INTERVAL '3 days');

-- +goose Down
DROP TABLE positions;
