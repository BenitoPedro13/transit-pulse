#!/bin/zsh
S=/private/tmp/claude-501/-Users-benito-Documents-personal-transit-pulse/910f162e-b970-40bc-ada1-ff7bc5f2c17e/scratchpad
# stop at 60 min after start (16:26:44Z), but only once E9 has recovered
until [ "$(date -u +%H%M%S)" -ge 162644 ] && grep -q "E9: redpanda healthy" $S/e7-events.log; do sleep 5; done
D=$(pgrep -f e7_driver.sh | head -1)
[ -n "$D" ] && { pkill -P $D; kill $D; }
P=$(pgrep adapter-its)
t0=$(python3 -I -c "import time;print(time.time())")
kill -INT $P
while kill -0 $P 2>/dev/null; do sleep 0.1; done
echo "$(date -u +%FT%TZ) final stop at 60 min (early_stop.sh); adapter exited after $(python3 -I -c "import time;print(round(time.time()-$t0,2))")s" | tee -a $S/e7-events.log
echo "$(date -u +%FT%TZ) done" >> $S/e7-events.log
