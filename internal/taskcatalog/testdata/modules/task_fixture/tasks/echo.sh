#!/bin/sh
# Prints the JSON it was given on stdin, wrapped in a result object.
input=$(cat)
printf '{"received":%s,"task":"%s"}\n' "$input" "${PT__task:-unset}"
