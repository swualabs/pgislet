import json
import sys

major, path = sys.argv[1:]
if major not in {"17", "18"}:
    raise SystemExit("Expected PostgreSQL major 17 or 18")

package = "github.com/swualabs/pgislet/tests/integration"
results = {}
package_passed = False

with open(path, encoding="utf-8") as stream:
    for line in stream:
        event = json.loads(line)
        if event.get("Package") != package:
            continue
        action = event.get("Action")
        test = event.get("Test")
        if test and action in {"pass", "fail", "skip"}:
            results[test] = action
        elif not test and action == "pass":
            package_passed = True

required = {
    "TestSupportedServerVersion": "pass",
    "TestSharedSQLCompatibility": "pass",
    "TestLifecycle": "pass",
    "TestPolicyRejectionAtomicity": "pass",
    "TestPolicyInitializationRollback": "pass",
    "TestPolicyLocalFunctionBatchLifecycle": "pass",
    "TestIsolationAndOwnership": "pass",
    "TestDistributedConcurrency": "pass",
    "TestProcessCrash": "pass",
    "TestPostgres18Features": "pass" if major == "18" else "skip",
}

if not package_passed:
    raise SystemExit("Integration package did not pass")

for test, expected in required.items():
    if results.get(test) != expected:
        raise SystemExit(f"{test}: expected {expected}, got {results.get(test, 'missing')}")

for test, result in results.items():
    if result != "pass" and not (test == "TestPostgres18Features" and major == "17" and result == "skip"):
        raise SystemExit(f"Unexpected integration result: {test}: {result}")

print(f"PostgreSQL {major}: verified {len(results)} integration test results")
