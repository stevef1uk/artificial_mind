#!/bin/bash
killall -9 hdn_server monitor_bin || true
sleep 1
(cd hdn && rm -f hdn_server && go build -o hdn_server . && nohup ./hdn_server >> hdn_debug.log 2>&1 &)
(cd monitor && rm -f monitor_bin && go build -o monitor_bin . && export MONITOR_STATIC_DIR=$(pwd)/static && nohup ./monitor_bin >> monitor_debug.log 2>&1 &)
