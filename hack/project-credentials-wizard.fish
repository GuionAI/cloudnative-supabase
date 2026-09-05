#!/usr/bin/env fish

set -g credential_bundle ""

function clear_clipboard
    printf '' | copy
    return $pipestatus[-1]
end

function clear_credential_clipboard --on-event fish_exit
    if type -q copy
        clear_clipboard
    end
    set -e credential_bundle
end

function fail --argument-names message
    printf '\nError: %s\n' "$message" >&2
    exit 1
end

function wait_for_enter --argument-names prompt
    read --local --prompt-str "$prompt" ignored
    or fail "input ended before the bundle was saved"
end

function copy_bundle_field --argument-names position field
    set --local value (printf '%s' "$credential_bundle" | jq --exit-status --raw-output --arg field "$field" '.[$field]')
    or fail "could not read $field from the generated bundle"

    printf '%s' "$value" | copy
    set --local copy_status $pipestatus[-1]
    set -e value
    test $copy_status -eq 0
    or fail "copy failed for $field"

    printf '\n[%s/5] %s is now in your clipboard.\n' "$position" "$field"
    wait_for_enter "Paste and save it under the exact key '$field', then press Enter: "
    clear_clipboard
    or fail "could not clear the clipboard after $field"
end

for dependency in go jq
    type -q "$dependency"
    or fail "required command '$dependency' was not found"
end
type -q copy
or fail "Fish function or command 'copy' was not found"

set --local repository_root (path resolve (dirname (status filename))/..)

printf 'CloudNative Supabase project credential bundle\n'
printf '==============================================\n\n'
printf 'This generates one atomic five-field bundle in memory.\n'
printf 'No secret is printed or written to disk. Each field is copied only when needed.\n\n'
wait_for_enter 'Open the target project/environment/path in your secret manager, then press Enter: '

printf '\nGenerating and validating the complete bundle...\n'
set -g credential_bundle (cd "$repository_root"; go run ./cmd/generate-project-credentials)
or fail "credential generation or operator validation failed"

printf '%s' "$credential_bundle" | jq --exit-status '
    type == "object" and
    (keys | sort) == (["anonRoleJwt", "publishableKey", "secretKey", "serviceRoleJwt", "signingKeys"] | sort) and
    all(.[]; type == "string" and length > 0)
' >/dev/null
or fail "generator returned an unexpected bundle"

printf 'Bundle validated. Keep all five values from this run together.\n'

copy_bundle_field 1 signingKeys
copy_bundle_field 2 publishableKey
copy_bundle_field 3 secretKey
copy_bundle_field 4 anonRoleJwt
copy_bundle_field 5 serviceRoleJwt

printf '\nAll five fields were copied in sequence.\n'
wait_for_enter 'Confirm the destination contains all five exact keys, then press Enter to finish: '
clear_clipboard
or fail "could not clear the clipboard"
set -e credential_bundle

printf '\nDone. The in-memory bundle and clipboard have been cleared.\n'
