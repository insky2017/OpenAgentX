export default {
  "test": {
    "root": "/tmp/oax-orca-assessment-20260925",
    "environment": "node",
    "include": [
      "src/shared/orchestration-ask-timeout.test.ts",
      "src/shared/agent-status-serving-readiness.test.ts",
      "src/shared/agent-status-store-reopen.test.ts",
      "src/shared/orchestration-fleet-evidence-clock.test.ts",
      "src/shared/orchestration-fleet-projection.test.ts",
      "src/shared/agent-status-run.test.ts",
      "src/shared/tui-agent-permissions.test.ts",
      "src/main/runtime/orchestration/db/attempt-outcome-projection.test.ts",
      "src/main/runtime/orchestration/db-stopping-worker-task-guard.test.ts"
    ],
    "maxWorkers": 2,
    "testTimeout": 30000,
    "hookTimeout": 30000
  }
}
