#!/bin/sh
# Renders each GoAT diagram source in this directory to an SVG beside it.
set -eu
cd "$(dirname "$0")"
for src in *.txt; do
	goat -i "$src" -o "${src%.txt}.svg" -sls '#000' font.css
done
