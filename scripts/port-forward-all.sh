#!/bin/bash

./scripts/port-forward-jaeger.sh &
./scripts/port-forward-bookbuyer-ui.sh &
./scripts/port-forward-bookthief-ui.sh &
./scripts/port-forward-osm-debug.sh &
./scripts/port-forward-grafana.sh &

VERSION=v2 ./scripts/port-forward-bookstore-ui-v2.sh &
VERSION=v1 ./scripts/port-forward-bookstore-ui-v1.sh &

./scripts/port-forward-bookbuyer-envoy.sh
./scripts/port-forward-bookstore-v1-envoy.sh


wait

