export default {
  root: '/tmp/paseo-assessment-20260923',
  cacheDir: '/tmp/paseo-assessment-checks/cache',
  test: { environment: 'node', fileParallelism: false, maxWorkers: 1, testTimeout: 10000, include: ['packages/server/src/server/agent/providers/provider-runner.test.ts', 'packages/server/src/server/agent/providers/jsonl-frame-decoder.test.ts'] }
};
