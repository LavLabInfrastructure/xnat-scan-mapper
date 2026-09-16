# xnat-scan-mapper

Container Service command that processes only custom forms named
`scan-map-<bundle>`. It reads explicitly configured scan-number mappings,
copies each mapped scan's complete `NIFTI` resource (including `.bval`,
`.bvec`, JSON sidecars, and nested files), and attaches `<bundle>.zip` to the
session.

## How it works

- `custom-form.json` — an example `scan-map-mapped_sessions` form with
   `t1ScanNumber`,
  `t1cScanNumber`, `t2ScanNumber`, `dwiScanNumber`, `adcScanNumber`), unset
  fields default to `-1`.
- `src/map_and_zip.py` — runs inside the container. It:
  1. Calls back into the XNAT REST API (`GET /data/experiments/{id}?format=json`)
   using the `XNAT_HOST`/`XNAT_USER`/`XNAT_PASS` credentials the Container
   Service automatically injects. `XNAT_API_HOST`, when set, overrides the
   public `XNAT_HOST` with a container-network-reachable URL. The command's
   Custom Forms API (`/xapi/custom-forms/forms` by default) lists every form
   whose `title` begins with `scan-map-`; each suffix becomes a bundle name.
   Set `XNAT_FORMS_API_PATH` or `--forms-api-path` when the plugin exposes a
   different forms-list route.
  2. Applies explicit, repeatable command-line rules:
     `--map formField:Destination`. The supplied command defines
     `t2ScanNumber:T2`, for example; `-1`/unset fields are skipped.
  3. For each selected scan, copies every file under
   `SCANS/<scan>/.../NIFTI/`. Root resource files whose name begins with
   `nifti` have only that prefix replaced by the map destination, so
   `nifti_a.nii.gz`, `nifti_b.nii.gz`, and `nifti.bval` become
   `T2/T2_a.nii.gz`, `T2/T2_b.nii.gz`, and `T2/T2.bval`. Other root files use
   the legacy mapping behavior (for example, `image.nii.gz` becomes
   `T2.nii.gz` and `image.bval` becomes `T2.bval`); nested paths are retained
   unchanged.
  4. Writes `/output/<bundle>.zip`, e.g. `mapped_sessions.zip`.
- `command.json` — the Container Service command/wrapper definition. It
   mounts the session's files read-only, supplies the mapping rules, passes
   the session ID into the script, and uploads every produced zip back onto
   the session as a resource.

## Configure a different form

Give each form a title beginning with `scan-map-`; the remaining title text is
the output bundle name. For example, `scan-map-brain-mri` creates
`brain-mri.zip`. Every matching form is processed during one command run.
Add a matching `--map <field-key>:<destination>` option for each scan-number
field used by the form family.
The destination is a simple directory/base-name. Configure dcm2niix with
`-f nifti` (typically `dcm2niix -z y -f nifti ...`); then
`--map flairScan:FLAIR` changes `nifti.nii.gz`, `nifti.bval`, and any
collision names such as `nifti_a.nii.gz` to `FLAIR/FLAIR.nii.gz`,
`FLAIR/FLAIR.bval`, and `FLAIR/FLAIR_a.nii.gz` respectively.

To support forms with different fields, add all their map rules to the same
command; fields absent from a given form are skipped. The standard XNAT
experiment endpoint returns saved field values but not Custom Forms schema
titles, so the mapper separately queries the Custom Forms list endpoint to
discover all bundles.
- `Dockerfile` — `python:3.11-slim` + `requests`.

## Build and register the image

```sh
cd /Users/mjbarrett/Code/xnat-scan-mapper
docker build -t xnat-scan-mapper:1.0 .
```

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

Now saving any `scan-map-<bundle>` form triggers the command, which discovers
all such forms, reads the just-saved field values straight from XNAT, creates
each `<bundle>.zip`, and attaches the results to the session's resources.

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
- The default Custom Forms schema-list endpoint is `/xapi/custom-forms/forms`.
   If your Custom Forms plugin uses another route, set `XNAT_FORMS_API_PATH` in
   `environment-variables` in `command.json` to that path (not a full URL).
- If your custom form fields aren't reflected in
  `/data/experiments/{id}?format=json`, adjust `fetch_custom_form_values()` in
  [src/map_and_zip.py](src/map_and_zip.py) to call the Forms plugin's
  dedicated values endpoint for your XNAT version instead.
