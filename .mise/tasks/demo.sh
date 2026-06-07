#!/bin/sh
# /// mise
# description = "Quick start for testing charge + example together."
# ///
#
# Usage:
#   mise run demo

set -e

CHARGE_PORT=8080
CHARGE_URL=http://localhost:$CHARGE_PORT
BE_PORT=8081
BE_CALLBACK_URL=http://localhost:$BE_PORT/callback

SIGNING_KEY_RAW=$(openssl ecparam -genkey -name prime256v1 -noout)
SIGNING_KEY=$(printf '%s' "$SIGNING_KEY_RAW" | sed ':a;N;$!ba;s/\n/\\n/g')

echo "starting charge on :$CHARGE_PORT"
CHARGE_DEPLOYMENT_URL=$CHARGE_URL \
CHARGE_SIGNING_KEYS="[{\"id\":\"dev-key\",\"pem\":\"$SIGNING_KEY\",\"alg\":\"ES256\"}]" \
CHARGE_PORT=$CHARGE_PORT \
CHARGE_ALLOW_INSECURE_ORIGINS=true \
go run ./cmd/charge &
CHARGE_PID=$!

sleep 1

echo "starting example backend on :$BE_PORT"
(
  cd example
  CHARGE_URL=$CHARGE_URL \
  CALLBACK_URL=$BE_CALLBACK_URL \
  PORT=$BE_PORT \
  go run server.go
) &
BE_PID=$!

echo ""
echo "charge:       $CHARGE_URL"
echo "example:      http://localhost:$BE_PORT"
echo "press Ctrl+C to stop"
echo ""

trap 'kill $CHARGE_PID $BE_PID 2>/dev/null; exit' INT TERM

wait
