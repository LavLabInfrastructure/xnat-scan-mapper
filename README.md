# xnat-scan-mapper

Container Service command that processes every active `scan-map-<bundle>`
custom form on a session. It reads explicitly configured scan-number
mappings, copies each mapped scan's complete `NIFTI` resource (including
`.bval`, `.bvec`, JSON sidecars, and nested files), and attaches
`<bundle>.zip` to the session for each active bundle.

## How it works

- `custom-form.json` — an example `scan-map-mapped_sessions` form. Its scan
   field keys include the bundle name, for example
   `scanMap_mapped_sessions_t2ScanNumber` (see below).
- `cmd/map-and-zip/main.go` — the dependency-free runtime implementation. It:
  1. Calls the documented XNAT Custom Fields API
     (`GET /xapi/custom-fields/experiments/{session}/fields`) using the
     `XNAT_HOST`/`XNAT_USER`/`XNAT_PASS` credentials the Container Service
     automatically injects. `XNAT_API_HOST`, when set, overrides the public
     `XNAT_HOST` with a container-network-reachable URL.
  2. This API returns a **flat** namespace of custom-field values with no
     indication of which form they came from. Each scan field therefore uses
     the key format `scanMap_<bundle>_<field-key>`. The mapper finds every
     persisted field with that format and derives the bundle name and mapping
     field automatically — no hidden marker or manual bundle configuration is
     required.
  3. Applies explicit, repeatable command-line rules:
     `--map formField:Destination`. The supplied command defines
     `t2ScanNumber:T2`, for example; `-1`/unset fields are skipped.
  4. For each selected scan, copies every file under
     `SCANS/<scan>/.../NIFTI/`. Root resource files whose name begins with
     `nifti` have only that prefix replaced by the map destination, so
     `nifti_a.nii.gz`, `nifti_b.nii.gz`, and `nifti.bval` become
     `T2/T2_a.nii.gz`, `T2/T2_b.nii.gz`, and `T2/T2.bval`. Other root files use
     the legacy mapping behavior (for example, `image.nii.gz` becomes
     `T2.nii.gz` and `image.bval` becomes `T2.bval`); nested paths are
     retained unchanged.
  5. Receives the XNAT session label directly from the wrapper and writes
     `/output/<session-label>_<bundle>.zip` for every active bundle, e.g.
     `101_MR_1_mapped_sessions.zip`. Unsafe label characters become
     underscores.
- `command.json` — the Container Service command/wrapper definition. It
  mounts the session's files read-only, supplies the mapping rules, passes
  the session ID into the script, and uploads every produced zip back onto
  the session as a resource.
- `src/map_and_zip.py` — retained as a readable Python reference implementation;
   it is not included in the runtime image.
- `Dockerfile` — multi-stage Go build with a distroless runtime image.

## Configure a different form

Give each form a title beginning with `scan-map-`; the remaining title text is
the output bundle name. For example, `scan-map-brain-mri` creates
`<session-label>_brain-mri.zip`.

The Custom Fields API persists values in one flat namespace, so encode this
bundle name into every scan field key. Use:

```text
scanMap_<bundle>_<field-key>
```

For the `scan-map-brain-mri` form, configure its T2 field as:

```json
{
   "key": "scanMap_brain-mri_t2ScanNumber",
   "type": "textfield",
   "input": true,
   "label": "T2 Scan Number",
   "placeholder": "-1"
}
```

The field-key suffix must equal the left side of an existing command mapping.
With `--map t2ScanNumber:T2`, the example field maps its selected scan to the
`T2` directory in `<session-label>_brain-mri.zip`. Every bundle is discovered
from persisted scan field keys; no hidden marker or `--bundle` arguments are
needed.

The destination is a simple directory/base-name. Configure dcm2niix with
`-f nifti` (typically `dcm2niix -z y -f nifti ...`); then
`--map flairScan:FLAIR` changes `nifti.nii.gz`, `nifti.bval`, and any
collision names such as `nifti_a.nii.gz` to `FLAIR/FLAIR.nii.gz`,
`FLAIR/FLAIR.bval`, and `FLAIR/FLAIR_a.nii.gz` respectively.

To support forms with different fields, add all their map rules to the same
command; fields absent from the saved values are simply skipped for that
session.

## Build and register the image

```sh
cd /Users/mjbarrett/Code/xnat-scan-mapper
docker build -t xnat-scan-mapper:1.0 .
```

The runtime image contains only the statically linked Go binary and its
certificate bundle; it has no Python interpreter, pip packages, or shell.

Push it somewhere your XNAT Docker server/host can pull from (or load it
directly on the same Docker host XNAT uses), then in XNAT:

1. **Administer > Plugin Settings / Images > Docker Server** — confirm the
   image is visible (`docker images` on the XNAT Docker host, or push to a
   registry XNAT can reach).
2. **Administer > Plugin Settings > Docker Commands (Container Service) >
   New Command > Upload from JSON`** — upload `command.json`, or use:

   ```sh
   curl -u admin:<password> -X POST \
     "https://<xnat-host>/xapi/commands" \
     -H "Content-Type: application/json" \
     --data @command.json
   ```

3. **Enable the command** for the project(s) that will use it: go to
   *Project > Manage > Automations* (or *Administer > Automation* site-wide)
   and enable the `map-and-zip-session-scans-wrapper` command for the
   project/site.

## Wire it to "form filled in" (auto-run)

The Container Service itself only launches on explicit REST calls or Event
Service triggers — there's no built-in "custom form saved" launch button, so
you need one Event Service automation per project (or site-wide):

1. **Administer (or Project) > Automation > Event Trigger > New Automation**.
2. **Event**: choose the session-update event XNAT fires when an experiment
   (and its custom form) is saved — on most 1.8.x installs this is listed as
   `SessionArchived`/`Session Data Changed`, or under the "Field(s) updated"
   event for `xnat:imageSessionData` if you're on a version that exposes
   custom-form saves as their own event. If your XNAT version doesn't expose
   a form-specific event, use "Session Data Changed", which also fires for a
   custom form save.
3. **Command**: `Map and Zip Session Scans`.
4. Leave the input blank / accept defaults — the wrapper only needs the
   `session` context input, which the Event Service supplies automatically
   from the event's subject.
5. Save and enable the automation.

Now saving a `scan-map-<bundle>` form triggers the command, which discovers
all active bundles from their `scanMapBundle_<bundle>` marker fields, reads
the just-saved values straight from XNAT, creates each `<bundle>.zip`, and
attaches the results to the session's resources.

## Notes / things to double check on your XNAT version

- The exact event name for "custom form saved" varies by XNAT/Forms-plugin
  version — check what's available in the Automation event dropdown and pick
  the closest session-level "updated"/"archived" event.
- `XNAT_HOST`/`XNAT_USER`/`XNAT_PASS` are injected automatically by the
   Container Service for REST callbacks. If the public `XNAT_HOST` is not
   resolvable inside containers, configure `XNAT_API_HOST` in the Docker Server
   / Container Service environment to an internal URL that is, such as
   `http://xnat:8080` when `xnat` is the Docker Compose service name. This is
   deployment-specific; verify it from the Docker network rather than assuming
   a particular hostname.
- If your custom form fields aren't reflected in
  `/data/experiments/{id}?format=json`, adjust `fetch_custom_form_values()` in
  [src/map_and_zip.py](src/map_and_zip.py) to call the Forms plugin's
  dedicated values endpoint for your XNAT version instead.
