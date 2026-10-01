export default {
  "test": {
    "root": "/tmp/oax-orca-assessment-20260925",
    "environment": "node",
    "include": [
      "src/main/native-chat/agent-session-journal/journal-restart-reconciliation.test.ts",
      "src/main/native-chat/agent-session-journal/journal-crash-boundary.test.ts",
      "src/main/runtime/orchestration/preamble.test.ts"
    ],
    "maxWorkers": 2,
    "testTimeout": 30000,
    "hookTimeout": 30000
  }
}
