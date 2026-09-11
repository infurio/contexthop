#!/bin/bash
# Every recording gets the same disposable scenario used by integration tests.
set -eu
: "${CONTEXTHOP_DEMO_BINARY:?Run scripts/record-demos to build and pin the demo binary}"
: "${CONTEXTHOP_FIXTURE_RUNNER:?Run scripts/record-demos to build the fixture runner}"
case "$CONTEXTHOP_FIXTURE_RUNNER" in
  /*) ;;
  *) echo 'The fixture runner must be an absolute path.' >&2; exit 1 ;;
esac
exec "$CONTEXTHOP_FIXTURE_RUNNER" --binary "$CONTEXTHOP_DEMO_BINARY" \
  --scenario acme --delay 1 -- /bin/bash --noprofile --norc
