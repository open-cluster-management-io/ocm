#!/usr/bin/env bash
# Prints one Markdown summary for all e2e jobs of a run, for $GITHUB_STEP_SUMMARY.
#
#   .github/scripts/e2e-summary.sh <dir>
#
# <dir> has one e2e-result-<job> directory per e2e job, with "status" (the job status), "job"
# (the job name) and, when the tests ran, the Ginkgo "junit.xml". download-artifact puts a lone
# artifact straight into <dir> instead, so <dir> itself is checked too.
set -uo pipefail

dir=${1:?usage: e2e-summary.sh <results dir>}

# Ginkgo writes one <testcase ... status="..."> per line and the failure message as an attribute
# of the <failure> element after it (<error> for panicked or interrupted specs). Prints "count <passed> <failed> <skipped>", then for each
# failed spec "spec <name>" followed by the first lines of its failure message.
parse_junit() {
  awk '
    function attr(s, a,   m) { if (match(s, a "=\"[^\"]*\"")) { m = substr(s, RSTART + length(a) + 2, RLENGTH - length(a) - 3); return m } return "" }
    function decode(s) {
      gsub(/&#xA;/, "\n", s); gsub(/&#34;/, "\"", s); gsub(/&#39;/, "\047", s)
      gsub(/&lt;/, "<", s); gsub(/&gt;/, ">", s); gsub(/&amp;/, "\\&", s); return s
    }
    /<testcase / {
      status = attr($0, "status")
      if (status == "passed") passed++
      else if (status == "skipped" || status == "pending") skipped++
      else { failed++; print "spec " decode(attr($0, "name")); want = 1 }
    }
    /<(failure|error) / && want {
      n = split(decode(attr($0, "message")), lines, "\n")
      # Some messages dump whole objects on one line; keep the summary readable.
      for (i = 1; i <= n && i <= 4; i++) print "msg " (length(lines[i]) > 300 ? substr(lines[i], 1, 300) "..." : lines[i])
      want = 0
    }
    END { printf "count %d %d %d\n", passed, failed, skipped }
  ' "$1"
}

suite_minutes() {
  grep -o '<testsuite [^>]*' "$1" | head -1 | grep -o ' time="[0-9.]*"' | grep -o '[0-9.]*' \
    | awk '{ printf "%d min", $1 / 60 + 0.5 }'
}

failures=""
echo "### E2E summary"
echo
echo "| Job | Result | Passed | Failed | Skipped | Test time |"
echo "|---|---|---|---|---|---|"
for result in "$dir" "$dir"/e2e-result-*; do
  [ -f "$result/status" ] || continue
  job=$(cat "$result/job" 2>/dev/null || echo "${result##*/e2e-result-}")
  status=$(cat "$result/status" 2>/dev/null || echo unknown)
  case $status in
    success) icon="✅" ;;
    failure) icon="❌" ;;
    cancelled) icon="⚪" ;;
    *) icon="❔" ;;
  esac
  if [ ! -s "$result/junit.xml" ]; then
    echo "| $job | $icon $status | - | - | - | - |"
    continue
  fi
  parsed=$(parse_junit "$result/junit.xml")
  read -r _ passed failed skipped <<< "$(grep '^count ' <<< "$parsed")"
  echo "| $job | $icon $status | $passed | $failed | $skipped | $(suite_minutes "$result/junit.xml") |"
  specs=$(awk -v job="$job" '
    /^spec / { if (open) print "```\n</details>\n"; printf "<details><summary><code>%s</code>: %s</summary>\n\n```\n", job, substr($0, 6); open = 1 }
    /^msg /  { print substr($0, 5) }
    END      { if (open) print "```\n</details>" }
  ' <<< "$parsed")
  [ -z "$specs" ] || failures+=$specs$'\n\n'
done

if [ -n "$failures" ]; then
  echo
  echo "**Failed specs** (agent logs are in the \`diagnostics-<job>\` artifact)"
  echo
  echo "$failures"
fi
