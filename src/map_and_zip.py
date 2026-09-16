#!/usr/bin/env python3
"""Create a named zip from files in NIFTI resources selected by a scan-map form."""
import argparse
import os
import shutil
import sys
import zipfile

import requests

UNSET_VALUES = {"", "-1", None}
FORM_PREFIX = "scan-map-"


def parse_mappings(mapping_args):
    mappings = {}
    for mapping_arg in mapping_args:
        try:
            form_key, destination = mapping_arg.split(":", 1)
        except ValueError as error:
            raise argparse.ArgumentTypeError(
                f"invalid mapping {mapping_arg!r}; use formKey:destination"
            ) from error
        if not form_key or not destination or "/" in destination or "\\" in destination:
            raise argparse.ArgumentTypeError(
                f"invalid mapping {mapping_arg!r}; destination must be a simple directory name"
            )
        mappings[form_key] = destination
    return mappings


def fetch_session_json(xnat_host, session_id, auth):
    url = f"{xnat_host.rstrip('/')}/data/experiments/{session_id}"
    response = requests.get(url, params={"format": "json"}, auth=auth, timeout=60)
    response.raise_for_status()
    return response.json()


def extract_scan_map_forms(payload):
    """Find custom-form objects whose title starts with scan-map-."""
    forms = []

    def walk(node):
        if isinstance(node, dict):
            title = node.get("title")
            if isinstance(title, str) and title.startswith(FORM_PREFIX):
                values = {}

                def collect_values(value_node):
                    if isinstance(value_node, dict):
                        for key, value in value_node.items():
                            if isinstance(value, (str, int, float)):
                                values[key] = str(value)
                            else:
                                collect_values(value)
                    elif isinstance(value_node, list):
                        for item in value_node:
                            collect_values(item)

                collect_values(node)
                forms.append((title[len(FORM_PREFIX):], values))
            for value in node.values():
                walk(value)
        elif isinstance(node, list):
            for item in node:
                walk(item)

    walk(payload)
    return forms


def find_nifti_resource(scan_dir):
    """Return the NIFTI resource directory for a mounted XNAT scan."""
    if not os.path.isdir(scan_dir):
        return None
    for root, directories, _ in os.walk(scan_dir):
        if os.path.basename(root).upper() == "NIFTI":
            return root
        directories.sort()
    return None


def mapped_filename(filename, label):
    """Preserve dcm2niix suffixes, otherwise use the legacy label basename."""
    if filename.startswith("nifti"):
        return f"{label}{filename[len('nifti'):]}"
    if filename.endswith(".nii.gz"):
        return f"{label}.nii.gz"
    extension = os.path.splitext(filename)[1]
    return f"{label}{extension}" if extension else filename


def copy_resource(resource_dir, destination_dir, label):
    """Copy all NIFTI-resource files, using mapped names for root files."""
    copied_count = 0
    for root, directories, files in os.walk(resource_dir):
        directories.sort()
        relative_dir = os.path.relpath(root, resource_dir)
        target_dir = destination_dir if relative_dir == "." else os.path.join(destination_dir, relative_dir)
        os.makedirs(target_dir, exist_ok=True)
        for filename in sorted(files):
            source = os.path.join(root, filename)
            if not os.path.isfile(source):
                continue
            target_filename = mapped_filename(filename, label) if relative_dir == "." else filename
            target = os.path.join(target_dir, target_filename)
            if os.path.exists(target):
                raise ValueError(f"multiple files map to {target}")
            shutil.copyfile(source, target)
            copied_count += 1
    return copied_count


def zip_directory(source_dir, zip_path):
    with zipfile.ZipFile(zip_path, "w", zipfile.ZIP_DEFLATED) as archive:
        for root, directories, files in os.walk(source_dir):
            directories.sort()
            for filename in sorted(files):
                source = os.path.join(root, filename)
                archive.write(source, os.path.relpath(source, source_dir))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--session-dir", required=True, help="Mounted session input directory")
    parser.add_argument("--output-dir", required=True, help="Directory for the generated bundle")
    parser.add_argument("--session-id", required=True, help="XNAT experiment ID of the session")
    parser.add_argument(
        "--map",
        action="append",
        required=True,
        metavar="FORM_KEY:DESTINATION",
        help="Map a form scan-number field to its directory in the zip; repeat for each field",
    )
    args = parser.parse_args()
    mappings = parse_mappings(args.map)

    xnat_host = os.environ.get("XNAT_HOST")
    xnat_user = os.environ.get("XNAT_USER")
    xnat_pass = os.environ.get("XNAT_PASS")
    if not all([xnat_host, xnat_user, xnat_pass]):
        parser.error("XNAT_HOST, XNAT_USER, and XNAT_PASS must be injected by Container Service")

    forms = extract_scan_map_forms(fetch_session_json(xnat_host, args.session_id, (xnat_user, xnat_pass)))
    if not forms:
        print(f"No custom form named {FORM_PREFIX}<bundle> was found; nothing to do.", file=sys.stderr)
        return

    os.makedirs(args.output_dir, exist_ok=True)
    for bundle_name, form_values in forms:
        if not bundle_name:
            print(f"warning: ignoring form named exactly {FORM_PREFIX}", file=sys.stderr)
            continue
        staging_dir = os.path.join(args.output_dir, f"_{bundle_name}")
        shutil.rmtree(staging_dir, ignore_errors=True)
        os.makedirs(staging_dir)
        copied_count = 0

        for form_key, destination in mappings.items():
            scan_number = form_values.get(form_key)
            if scan_number in UNSET_VALUES or str(scan_number).strip() in UNSET_VALUES:
                continue
            resource_dir = find_nifti_resource(
                os.path.join(args.session_dir, "SCANS", str(scan_number).strip())
            )
            if not resource_dir:
                print(f"warning: no NIFTI resource for scan {scan_number} ({form_key}); skipping", file=sys.stderr)
                continue
            copied_count += copy_resource(
                resource_dir, os.path.join(staging_dir, destination), destination
            )

        if not copied_count:
            print(f"warning: {bundle_name} had no mapped NIFTI files; no zip created", file=sys.stderr)
            shutil.rmtree(staging_dir, ignore_errors=True)
            continue

        zip_path = os.path.join(args.output_dir, f"{bundle_name}.zip")
        zip_directory(staging_dir, zip_path)
        shutil.rmtree(staging_dir, ignore_errors=True)
        print(f"Wrote {zip_path} with {copied_count} file(s).")


if __name__ == "__main__":
    main()
