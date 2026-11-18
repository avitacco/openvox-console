#!/bin/sh
sleep "$PT_seconds" &
echo $! > "${TMPDIR:-/tmp}/task_fixture_sleep.pid"
wait
echo '{"slept":true}'
