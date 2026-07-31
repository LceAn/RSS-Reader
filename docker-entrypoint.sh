#!/bin/sh
set -eu

config_path="${RSS_READER_CONFIG:-/app/data/config.json}"
config_seed="${RSS_READER_CONFIG_SEED:-/app/config.example.json}"

if [ ! -f "$config_path" ]; then
    mkdir -p "$(dirname "$config_path")"
    cp "$config_seed" "$config_path"
    chmod 600 "$config_path"
fi

exec /app/rss-reader "$@"
