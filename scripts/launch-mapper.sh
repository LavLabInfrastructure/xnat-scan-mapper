#!/usr/bin/env sh
set -eu

: "${XNAT_HOST:?Set XNAT_HOST to the XNAT base URL}"
: "${XNAT_USER:?Set XNAT_USER to an XNAT user with launch permission}"
: "${XNAT_PASS:?Set XNAT_PASS for XNAT_USER}"
: "${XNAT_PROJECT:?Set XNAT_PROJECT to the session project ID}"
: "${XNAT_COMMAND_ID:?Set XNAT_COMMAND_ID to the command ID}"
: "${XNAT_WRAPPER_NAME:=map-and-zip-session-scans-wrapper}"
: "${SESSION_URI:?Set SESSION_URI to /experiments/<session-id>}"

case "$SESSION_URI" in
  /experiments/*) ;;
  *)
    printf '%s\n' "SESSION_URI must be /experiments/<session-id>, got: $SESSION_URI" >&2
    exit 2
    ;;
esac

curl --fail-with-body --silent --show-error \
  --user "$XNAT_USER:$XNAT_PASS" \
  --header 'Content-Type: application/json' \
  --request POST \
  --data "{\"session\":\"$SESSION_URI\"}" \
  "$XNAT_HOST/xapi/projects/$XNAT_PROJECT/commands/$XNAT_COMMAND_ID/wrappers/$XNAT_WRAPPER_NAME/launch"
