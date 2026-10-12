#!/bin/zsh
# Throwaway choreography for E7/E8/E9 (TASK-its-adapter §5). Usage: e7_driver.sh <minute-lag>
# Timeline (minutes since start):
#   0     start adapter, fresh raw dir, 1h backfill window
#   25    E8: SIGINT, wait 5 min, restart
#   50    E9: stop Redpanda 3 min, start it again
#   >=82  final SIGINT (60+ live minutes after the catch-up, including the pauses)
S=/private/tmp/claude-501/-Users-benito-Documents-personal-transit-pulse/910f162e-b970-40bc-ada1-ff7bc5f2c17e/scratchpad
LAG=${1:-3m}
RAW=$S/e7-raw
EV=$S/e7-events.log
rm -rf $RAW $S/e7.log $S/e7-rss.log $EV
mark() { echo "$(date -u +%FT%TZ) $*" | tee -a $EV; }

start_adapter() {
  (cd $S && RAW_DIR=$RAW ITS_BACKFILL_WINDOW=1h ITS_MINUTE_LAG=$LAG nohup ./adapter-its >> $S/e7.log 2>&1 &)
  sleep 2
  mark "adapter started pid=$(pgrep adapter-its)"
}
stop_adapter() {
  local p=$(pgrep adapter-its)
  local t0=$(python3 -I -c "import time;print(time.time())")
  kill -INT $p
  while kill -0 $p 2>/dev/null; do sleep 0.1; done
  mark "adapter stopped after $(python3 -I -c "print(round(time.time()-$t0,2))")s"
}

docker exec transit-pulse-redpanda-1 rpk topic delete positions.v1 feed-status.v1 >/dev/null 2>&1
mark "topics deleted; starting E7 with lag=$LAG"
start_adapter
t_start=$(date +%s)

# RSS sample every minute (kB), pid of whichever adapter is running
( while true; do
    p=$(pgrep adapter-its); [ -n "$p" ] && echo "$(date -u +%FT%TZ) $(ps -o rss= -p $p | tr -d ' ')" >> $S/e7-rss.log
    sleep 60
  done ) &
RSS_LOOP=$!

wait_until() { while [ $(( $(date +%s) - t_start )) -lt $(( $1 * 60 )) ]; do sleep 5; done; }

wait_until 25
mark "E8: stopping adapter for 5 minutes"
stop_adapter
sleep 300
mark "E8: restarting adapter"
start_adapter

wait_until 50
mark "E9: stopping redpanda for 3 minutes"
docker stop transit-pulse-redpanda-1 >/dev/null
sleep 180
mark "E9: starting redpanda"
docker start transit-pulse-redpanda-1 >/dev/null
until docker exec transit-pulse-redpanda-1 rpk cluster health 2>/dev/null | grep -q "Healthy:.*true"; do sleep 3; done
mark "E9: redpanda healthy"

wait_until 82
mark "final stop"
stop_adapter
kill $RSS_LOOP
mark "done"
